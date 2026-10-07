package deps

import (
	_ "embed"
	"bufio"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/env"
	"pypacker/internal/paths"
)

//go:embed pipreqs_mapping.txt
var pipreqsMapping string

// pyarmorVerRe 匹配 PyArmor 版本号，如 "9.2.6" / "8.5.8"
var pyarmorVerRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// CheckInstalled 检测目标 Python 环境是否已安装指定 pip 包，返回是否已装与版本号。
// 用隐藏窗口执行，避免 GUI 内弹出黑框；版本为空且无报错时按"未安装"处理。
func CheckInstalled(pythonPath, pkg string) (bool, string) {
	script := "import sys, importlib.metadata as m\nsys.stdout.reconfigure(encoding='utf-8')\ntry:\n    print(m.version('" + pkg + "'))\nexcept Exception:\n    pass\n"
	cmd := cmdutil.Command(pythonPath, "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, ""
	}
	ver := strings.TrimSpace(string(out))
	return ver != "", ver
}

// FindPyarmor 定位所选 Python 环境中的 pyarmor 可执行文件（统一转发到 env 包实现）
func FindPyarmor(pythonPath string) string {
	return env.FindPyarmor(pythonPath)
}

// CheckPyarmor 检测目标 Python 环境是否已安装 PyArmor（代码加密用），返回是否可用及版本信息。
// 使用隐藏窗口执行，避免 GUI 内弹出黑框。
func CheckPyarmor(pythonPath string) (bool, string) {
	exe := env.FindPyarmor(pythonPath)
	if exe == "" {
		return false, ""
	}
	cmd := cmdutil.Command(exe, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, ""
	}
	// 版本信息通常在第一行，形如 "Pyarmor 9.2.6 (trial), 000000, non-profits"
	first := strings.TrimSpace(strings.Split(strings.TrimSpace(string(out)), "\n")[0])
	return true, extractPyarmorVersion(first)
}

// extractPyarmorVersion 从 "Pyarmor 9.2.6 (trial)..." 中提取纯版本号 "9.2.6"，
// 避免把许可证信息（trial/000000/non-profits）显示在界面上
func extractPyarmorVersion(s string) string {
	m := pyarmorVerRe.FindString(s)
	if m == "" {
		return s
	}
	return m
}

// analyzeScript 内嵌 Python 脚本：基于 PyInstaller 官方分析引擎的依赖分析。
//
// 原理（比旧版"纯静态 AST 扫描"更准）：
//  1. 候选提取：先用 AST 扫描入口脚本 + 递归本地模块，收集代码里 import 的顶层模块名
//     （这是"程序引用了什么"；PyInstaller 的 missing 清单含大量平台/hook 噪音，不能直接用，
//     所以缺失判断必须以代码 import 候选为准）
//  2. 真相来源：调用 PyInstaller 官方 Analysis API（与正式打包同一引擎），它做字节码扫描、
//     递归展开依赖树、执行上千个官方 hook，输出"实际能收集到的模块"（含动态导入补充）
//  3. 差集判定：
//     - 候选命中 Analysis 收集结果 → 已安装
//     - 候选未命中、且非标准库/非本地模块 → 真缺失
//     - Analysis 收集到、但代码没直接 import 的第三方模块 → "钩子补充"（间接依赖，
//       随主包 pip 自动安装，仅展示）
//  4. analysisModules：Analysis 收集的全部第三方模块全名，供加密打包时全量喂给
//     PyInstaller 收集阶段（加密后 PyInstaller 看不到 import，必须用这份清单替它补齐）
//
// 注意：Analysis 的 INFO 日志输出到 stderr，Go 侧逐行转发做进度显示；
// stdout 只输出最后一个 JSON 行。
const analyzeScript = `
import ast, json, os, re, sys, time, importlib.metadata, importlib.util

# 强制 UTF-8 输出，保证中文包名不乱码
try:
    sys.stdout.reconfigure(encoding='utf-8')
except Exception:
    pass

def is_stdlib(name):
    try:
        return name in sys.stdlib_module_names
    except AttributeError:
        return False

def norm(name):
    # 归一化包名用于匹配（忽略大小写、连字符/下划线差异）
    return name.lower().replace('-', '').replace('_', '')

def parse_mapping(raw):
    """解析 pipreqs 官方映射表（每行 模块名:包名）：
    - key 统一转小写（import PIL 和 import pil 都能命中）
    - 包名统一转小写 + 下划线转连字符（scikit_learn -> scikit-learn，
      Requests -> requests，pip 安装时大小写不敏感，小写为规范形式）
    """
    m = {}
    for line in raw.splitlines():
        line = line.strip()
        if not line or ':' not in line:
            continue
        mod, pkg = line.split(':', 1)
        mod = mod.strip().lower()
        pkg = pkg.strip().replace('_', '-').lower()
        if mod and pkg:
            m[mod] = pkg
    return m

# 官方映射表（pipreqs 0.5.0 自带，1152 条，由 Go 侧 go:embed 注入）
OFFICIAL_RAW = r'''__PIPREQS_MAPPING__'''
MODULE_TO_PKG = parse_mapping(OFFICIAL_RAW)

# 官方表缺失的补充（Windows 专属与常见遗漏）：
MODULE_TO_PKG.update({
    'cv2': 'opencv-python',
    'win32api': 'pywin32', 'win32gui': 'pywin32', 'win32process': 'pywin32',
    'win32com': 'pywin32', 'win32con': 'pywin32', 'win32event': 'pywin32',
    'win32file': 'pywin32', 'win32pipe': 'pywin32', 'win32security': 'pywin32',
    'win32service': 'pywin32', 'win32ts': 'pywin32', 'win32clipboard': 'pywin32',
    'win32evtlog': 'pywin32', 'win32net': 'pywin32', 'win32print': 'pywin32',
    'win32ui': 'pywin32', 'pywintypes': 'pywin32', 'pythoncom': 'pywin32',
    'skimage': 'scikit-image',
    'nacl': 'pynacl',
    'pkg_resources': 'setuptools',
})

def parse_req(path):
    """解析 requirements.txt，返回 {归一化包名: 版本约束}，如 {'numpy': '==1.26.0'}"""
    constraints = {}
    try:
        with open(path, 'r', encoding='utf-8', errors='ignore') as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                if '#' in line:
                    line = line.split('#', 1)[0].strip()
                if not line:
                    continue
                if line.startswith(('-r', '-e', '-c', '--')):
                    continue
                line = re.sub(r'\[[^\]]*\]', '', line)
                m = re.search(r'(==|>=|<=|!=|~=|<|>)', line)
                if m:
                    name, ver = line[:m.start()].strip(), line[m.start():].strip()
                    constraints[norm(name)] = ver
                else:
                    constraints[norm(line.strip())] = ''
    except Exception:
        pass
    return constraints

def main(entry, workpath):
    t0 = time.time()
    entry = os.path.abspath(entry)
    base_dir = os.path.dirname(entry)
    # 读取同目录 requirements.txt 的版本约束（没有该文件则无约束，装最新版）
    constraints = parse_req(os.path.join(base_dir, 'requirements.txt'))

    # ---------- 1. PyInstaller 官方分析引擎（与正式打包同一套） ----------
    # workpath 按项目分目录（Go 侧传入）：PyInstaller 自带"结果未变则复用缓存"，
    # 重复分析（如加密打包时）秒回；缓存按项目隔离，切项目不互相覆盖。
    from PyInstaller.config import CONF
    from PyInstaller.building.build_main import Analysis
    spec = os.path.join(workpath, 'deps.spec')
    CONF['spec'] = spec
    CONF['specpath'], CONF['specnm'] = os.path.split(spec)
    CONF['specnm'] = os.path.splitext(CONF['specnm'])[0]
    CONF['distpath'] = os.path.join(workpath, 'dist')
    CONF['workpath'] = os.path.join(workpath, 'build')
    CONF['warnfile'] = os.path.join(CONF['workpath'], 'warn-%s.txt' % CONF['specnm'])
    CONF['dot-file'] = os.path.join(CONF['workpath'], 'graph-%s.dot' % CONF['specnm'])
    CONF['xref-file'] = os.path.join(CONF['workpath'], 'xref-%s.html' % CONF['specnm'])
    CONF['code_cache'] = dict()
    os.makedirs(CONF['workpath'], exist_ok=True)
    os.makedirs(CONF['distpath'], exist_ok=True)

    a = Analysis([entry], pathex=[base_dir], binaries=[], datas=[],
                 hiddenimports=[], hookspath=[], runtime_hooks=[],
                 excludes=[], noarchive=False)
    dt = time.time() - t0
    is_cached = dt < 3.0  # 命中 PyInstaller 缓存时通常 1 秒内返回

    # ---------- 2. 已收集模块集合 ----------
    # 注意：analysisModules 只使用纯 Python 模块（pure）的模块名；
    # binaries 的 name 是收集目标路径（如 "win32/win32gui.pyd"），不适合当 --hidden-import，
    # 扩展模块由 --collect-all 包名 + 顶层 hidden-import 覆盖。
    collected = {}
    pure_names = set()
    for name, path, tc in a.pure:
        collected[name] = ('pure', path)
        pure_names.add(name)
    for name, path, tc in a.binaries:
        collected[name] = ('bin', path)

    def is_local_mod(mod):
        info = collected.get(mod)
        if not info:
            return False
        path = info[1] or ''
        if not path:
            return False
        return os.path.normpath(path).lower().startswith(os.path.normpath(base_dir).lower())

    def is_third_party(name, path):
        # 第三方：路径在 site-packages 里，或本身就是 .pyd 扩展模块
        if not path:
            return False
        p = str(path).replace('\\', '/')
        return 'site-packages' in p or p.endswith('.pyd')

    # ---------- 3. 候选提取 ----------
    # 本地模块发现交给 PyInstaller：它的收集结果里已经包含"被 import 链上的全部本地文件"
    # （含包内相对导入 from . import xxx 的模块），比我们手写递归更全更省事。
    # 对这些本地文件做 AST 提取 import 名，并记录来源文件与行号（供界面展示）。
    local_files = set()
    for name, info in collected.items():
        if info[0] != 'pure':
            continue
        path = info[1] or ''
        if not path or not path.endswith('.py'):
            continue
        if os.path.normpath(path).lower().startswith(os.path.normpath(base_dir).lower()):
            local_files.add(os.path.normpath(path))
    local_files.add(os.path.normpath(entry))  # 入口必扫

    imports_src = {}  # 顶层 import 名 -> set(来源 "相对路径:行号")
    scanned = set()

    def scan(path):
        if path in scanned or len(scanned) > 300:
            return
        scanned.add(path)
        try:
            with open(path, "r", encoding="utf-8-sig", errors="ignore") as f:
                tree = ast.parse(f.read(), path)
        except Exception:
            return
        try:
            rel = os.path.relpath(path, base_dir)
        except Exception:
            rel = os.path.basename(path)

        def record(mod, lineno):
            top = mod.split('.')[0]
            imports_src.setdefault(top, set()).add('%s:%d' % (rel, lineno))

        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for a in node.names:
                    record(a.name, node.lineno)
            elif isinstance(node, ast.ImportFrom) and node.module:
                record(node.module, node.lineno)
            elif isinstance(node, ast.Call):
                # 动态导入启发式：importlib.import_module("x") / __import__("x")
                fn = node.func
                if isinstance(fn, ast.Attribute) and fn.attr == "import_module":
                    if node.args and isinstance(node.args[0], ast.Constant) and isinstance(node.args[0].value, str):
                        record(node.args[0].value, node.lineno)
                elif isinstance(fn, ast.Name) and fn.id == "__import__":
                    if node.args and isinstance(node.args[0], ast.Constant) and isinstance(node.args[0].value, str):
                        record(node.args[0].value, node.lineno)

    for f in sorted(local_files):
        scan(f)

    # ---------- 4. 包名映射 ----------
    installed_dists = {d.metadata["Name"].lower() for d in importlib.metadata.distributions()}
    pkgs_map = {}
    try:
        pkgs_map = importlib.metadata.packages_distributions()
    except Exception:
        pass

    def to_pkg(mod):
        key = mod.lower()
        pkg = MODULE_TO_PKG.get(key)
        if pkg is None:
            dists = pkgs_map.get(mod, [])
            # 发行版名转小写，统一成 pip 可识别的规范名（如 "Requests" -> "requests"）
            pkg = (dists[0] if dists else mod).lower()
        return pkg

    def req_version(dists, pkg):
        for d in dists:
            if norm(d) in constraints:
                return constraints[norm(d)]
        if norm(pkg) in constraints:
            return constraints[norm(pkg)]
        return ''

    def installed_ver(dists, pkg):
        try:
            for d in dists:
                if d.lower() in installed_dists:
                    return importlib.metadata.version(d)
            if pkg.lower() in installed_dists:
                return importlib.metadata.version(pkg)
        except Exception:
            pass
        return ''

    # ---------- 5. 差集判定 ----------
    stdlib = set(sys.stdlib_module_names)

    def has_module(imp):
        """权威兜底：直接问当前 Python 的 import 机制"这个模块能否被定位"。
        完全通用，不依赖 PyInstaller 的收集命名规则（纯模块名 / 二进制路径名 /
        其他任何形式），只要环境里真实存在（可 import），就判已装。"""
        try:
            return importlib.util.find_spec(imp) is not None
        except (ImportError, ValueError, AttributeError, ModuleNotFoundError):
            return False

    def hit_mod(imp):
        """PyInstaller 收集判定：环境里有没有该模块。
        收集结果含两类名字：
          - 纯模块名：cv2.core、numpy 等（a.pure）
          - 二进制路径名：cv2\\cv2.pyd、win32\\win32api.pyd（a.binaries）
        只查模块名会漏掉纯扩展模块（cv2 / win32api 这类），因此还要按目录前缀
        （cv2\\ 开头）和文件名（win32api.pyd）双向匹配。"""
        if imp in collected:
            return True
        if any(k.startswith(imp + '.') for k in collected):
            return True
        for k in collected:
            kp = k.replace('\\', '/')
            if kp.startswith(imp + '/'):
                return True
            if kp.rsplit('/', 1)[-1] in (imp + '.pyd', imp + '.dll', imp + '.so'):
                return True
        return False

    groups_missing = {}
    groups_resolved = {}
    for imp, srcs in imports_src.items():
        if imp in sys.builtin_module_names or is_stdlib(imp) or is_local_mod(imp):
            continue
        # 命中 Analysis 收集结果 或 环境可加载 → 已装；否则真缺失
        hit = hit_mod(imp) or has_module(imp)
        pkg = to_pkg(imp)
        g = groups_missing if not hit else groups_resolved
        gk = norm(pkg)
        if gk not in g:
            g[gk] = {"pkg": pkg, "mods": set(), "srcs": set()}
        g[gk]["mods"].add(imp)
        g[gk]["srcs"].update(srcs)

    def build_items(groups, installed_flag):
        items = []
        for gk in sorted(groups):
            g = groups[gk]
            pkg = g["pkg"]
            mods = sorted(g["mods"])
            dists = pkgs_map.get(mods[0], [])
            # 来源去重排序，最多展示 4 处（超出以「等 N 处」省略）
            srcs = sorted(g["srcs"])
            src_txt = ', '.join(srcs[:4]) + (' 等 %d 处' % len(srcs) if len(srcs) > 4 else '')
            items.append({
                "importName": ", ".join(mods),
                "pkgName": pkg,
                "version": req_version(dists, pkg),
                "installedVersion": installed_ver(dists, pkg),
                "installed": installed_flag,
                "source": src_txt,
            })
        return items

    missing = build_items(groups_missing, False)
    resolved = build_items(groups_resolved, True)

    # ---------- 6. 钩子补充（间接依赖：Analysis 收集到但代码未直接 import） ----------
    hook_groups = {}
    for name in pure_names:
        top = name.split('.')[0]
        info = collected[name]
        path = info[1] or ''
        if info[0] != 'pure':
            continue
        if top in stdlib or top in imports_src or top.startswith('_'):
            continue
        if name.startswith('pyimod') or name.startswith('pyi_rth') or top == 'PyInstaller':
            continue
        if is_local_mod(name) or not is_third_party(name, path):
            continue
        pkg = to_pkg(top)
        if norm(pkg) in ('pyinstaller', 'pyarmor'):
            continue
        gk = norm(pkg)
        if gk not in hook_groups:
            hook_groups[gk] = {"pkg": pkg, "mods": set(), "srcs": set()}
        # 只记顶层模块名，避免 importName 拼接上百个子模块
        hook_groups[gk]["mods"].add(top)
    hook_extra = build_items(hook_groups, True)

    # ---------- 7. analysisModules：全部第三方模块全名（加密打包收集用） ----------
    # 只取纯 Python 模块（模块名形式，可安全作为 --hidden-import）；
    # 排除：标准库 / 本地模块 / PyInstaller 运行时 / 下划线开头的内部模块
    analysis_modules = []
    for name in pure_names:
        top = name.split('.')[0]
        info = collected[name]
        path = info[1] or ''
        if top in stdlib or top in sys.builtin_module_names or top.startswith('_'):
            continue
        if name.startswith('pyimod') or name.startswith('pyi_rth') or top == 'PyInstaller':
            continue
        if is_local_mod(name) or not is_third_party(name, path):
            continue
        analysis_modules.append(name)
    # 二进制扩展模块（cv2.pyd / win32api.pyd 等）：纯模块收集里没有它们，
    # 加密打包时 PyInstaller 看不到 import，必须 --hidden-import 激活对应钩子才能收集。
    # 取文件名的 .pyd 部分（去掉扩展名且为合法模块标识符），可安全作为 --hidden-import。
    for name, path, tc in a.binaries:
        base = name.replace('\\', '/').rsplit('/', 1)[-1]
        if base.endswith('.pyd'):
            mname = base[:-4]
            if mname and mname not in stdlib and re.match(r'^[A-Za-z_][A-Za-z0-9_]*$', mname):
                analysis_modules.append(mname)
    analysis_modules = sorted(set(analysis_modules))

    print(json.dumps({
        "missing": missing,
        "resolved": resolved,
        "hookExtra": hook_extra,
        "analysisModules": analysis_modules,
        "analysisSeconds": round(dt, 1),
        "isCached": is_cached,
    }, ensure_ascii=False))

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
`

// PyiCacheDir 返回某入口脚本专属的 PyInstaller 分析缓存目录（工具数据目录下）。
// 按入口路径哈希分目录：每个项目的缓存互不干扰，切回老项目时命中各自缓存秒回
// （PyInstaller 自带"入口/依赖未变则复用分析结果"的缓存，无需自己实现失效逻辑）。
func PyiCacheDir(entryScript string) string {
	sum := md5.Sum([]byte(filepath.Clean(entryScript)))
	dir := filepath.Join(paths.PyiCacheRoot(), fmt.Sprintf("%x", sum)[:16])
	os.MkdirAll(dir, 0o755)
	return dir
}

// maxPyiCacheProjects 保留最近使用过的项目分析缓存数，超出自动清理最旧的，
// 防止长期使用缓存目录无限膨胀（缓存本身每个项目仅几 MB，此上限兜底）。
const maxPyiCacheProjects = 10

// CleanPyiCache 保留最近 maxPyiCacheProjects 个项目的分析缓存，删除更旧的目录。
// 每次分析结束后调用一次；删除的只是缓存，下次分析会自动重建（多等几秒）。
func CleanPyiCache() {
	root := paths.PyiCacheRoot()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) <= maxPyiCacheProjects {
		return
	}
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{path: filepath.Join(root, e.Name()), mod: fi.ModTime()})
	}
	if len(items) <= maxPyiCacheProjects {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	for _, it := range items[maxPyiCacheProjects:] {
		os.RemoveAll(it.path)
	}
}

// Analyze 基于 PyInstaller 官方分析引擎分析入口脚本的第三方依赖：
//   - 目标 Python 环境执行（隐藏窗口不闪黑框）
//   - onProgress 非空时，逐行转发分析引擎的实时日志（供界面做进度显示）
//   - 返回缺失/已装/钩子补充清单，以及供加密打包使用的全量第三方模块清单
func Analyze(entryScript, pythonPath string, onProgress func(string)) (*config.DepsResult, error) {
	if entryScript == "" || pythonPath == "" {
		return nil, fmt.Errorf("入口脚本和 Python 路径不能为空")
	}
	// 把 go:embed 的官方映射表注入脚本（占位符替换；映射表为纯 ASCII，无引号冲突）
	script := strings.Replace(analyzeScript, "__PIPREQS_MAPPING__", pipreqsMapping, 1)
	// 脚本含 1152 条映射，体积大：用 -c 传参会超过 Windows 命令行长度限制，
	// 改为写入临时脚本文件再执行（用完即删）。
	tmp, err := os.CreateTemp("", "deps_scan_*.py")
	if err != nil {
		return nil, fmt.Errorf("创建临时分析脚本失败: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("写入临时分析脚本失败: %v", err)
	}
	tmp.Close()

	cmd := cmdutil.Command(pythonPath, tmpName, entryScript, PyiCacheDir(entryScript))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建输出管道失败: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建错误管道失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动依赖分析失败: %v", err)
	}

	// 逐行读取 stdout/stderr：JSON 行是最终结果，其余行是分析引擎日志（转发给界面做进度）；
	// stderr 全文同时累积，供失败时拼装中文诊断（分析脚本崩溃的 traceback 一定在 stderr）
	var jsonLine string
	var errOut strings.Builder
	var mu sync.Mutex
	readPipe := func(rc io.ReadCloser, isErr bool) {
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if !isErr && strings.HasPrefix(line, "{") {
				mu.Lock()
				jsonLine = line
				mu.Unlock()
				continue
			}
			if isErr {
				mu.Lock()
				errOut.WriteString(line + "\n")
				mu.Unlock()
			}
			if onProgress != nil {
				onProgress(line)
			}
		}
	}
	go readPipe(stdout, false)
	go readPipe(stderr, true)

	err = cmd.Wait()
	if err != nil {
		mu.Lock()
		final := jsonLine
		mu.Unlock()
		if final == "" {
			// 分析脚本崩溃且无结果：用中文诊断（原因+建议+最近输出原文）替代干巴巴的退出码
			return nil, fmt.Errorf("依赖分析失败: %v\n%s", err, BuildAnalyzeDiagnosis(errOut.String()))
		}
		// 有 JSON 输出但进程退出码非 0（如 PyInstaller 缓存写警告），仍尝试解析
		var res config.DepsResult
		if e := json.Unmarshal([]byte(final), &res); e == nil {
			CleanPyiCache()
			return &res, nil
		}
		return nil, fmt.Errorf("依赖分析失败: %v", err)
	}

	mu.Lock()
	final := jsonLine
	mu.Unlock()
	if final == "" {
		return nil, fmt.Errorf("依赖分析未产生结果\n%s", BuildAnalyzeDiagnosis(errOut.String()))
	}
	var res config.DepsResult
	if e := json.Unmarshal([]byte(final), &res); e != nil {
		return nil, fmt.Errorf("依赖分析结果解析失败: %v", e)
	}
	CleanPyiCache()
	return &res, nil
}
