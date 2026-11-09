# 依赖分析脚本：由 Go 侧 go:embed 读取，写入临时文件后用目标 Python 解释器执行。
# 调用约定：argv[1] = 入口脚本路径，argv[2] = 分析缓存目录；结果以单行 JSON 输出到 stdout。
# 原理与字段说明见 internal/deps/scan.go 中 Analyze 的注释。
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
    # 上次的分析结果是否存在（PyInstaller 会复用 build 目录下的 Analysis-00.toc）
    toc_file = os.path.join(CONF['workpath'], 'Analysis-00.toc')
    had_cache = os.path.isfile(toc_file)

    a = Analysis([entry], pathex=[base_dir], binaries=[], datas=[],
                 hiddenimports=[], hookspath=[], runtime_hooks=[],
                 excludes=[], noarchive=False)
    dt = time.time() - t0
    # 只有"上次结果存在 + 本次很快返回"才算命中缓存；
    # 单看耗时会把小型项目的首次分析（引擎加载本身也要几秒）误标成命中
    is_cached = had_cache and dt < 3.0

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

    def is_site_packages(path):
        # 第三方库统一装在 site-packages（Windows/macOS）或 dist-packages（Linux）下，
        # 路径命中即视为第三方库文件，不是用户自己的本地模块
        p = str(path).replace('\\', '/').lower()
        return '/site-packages/' in p or '/dist-packages/' in p

    def is_local_mod(mod):
        info = collected.get(mod)
        if not info:
            return False
        path = info[1] or ''
        if not path:
            return False
        if is_site_packages(path):
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
        # 只扫描用户自己的本地文件；site-packages 里的第三方库文件即使
        # 落在项目目录内（如 venv 就建在项目里），其内部 import 也不算依赖
        if is_site_packages(path):
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
            elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
                # level>0 是相对导入（from . import x / from .m import y），
                # 那是包内部模块之间的互相引用，不是需要单独 pip 安装的依赖
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
    # 注意：个别发行版的元数据缺失 Name（打包不规范），直接 d.metadata["Name"].lower()
    # 会抛 AttributeError 让整个分析崩掉，这里逐个兜底跳过。
    installed_dists = set()
    for d in importlib.metadata.distributions():
        try:
            name = (d.metadata.get('Name') or '').strip().lower()
        except Exception:
            continue
        if name:
            installed_dists.add(name)
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
