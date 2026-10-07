package env

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/paths"
)

// detectCache 环境检测短时缓存：同一 Python 环境 30 秒内重复检测直接复用结果，
// 避免切换页面/重复搜索时反复启动子进程（每次检测 5~6 次进程冷启动非常慢）。
var (
	detectCache   = map[string]detectCacheItem{}
	detectCacheMu sync.Mutex
)

type detectCacheItem struct {
	at   time.Time
	env  config.PythonEnv
	err  error
}

// detectCacheTTL 检测结果缓存有效期（秒）
const detectCacheTTL = 30

// detectScript 合并检测脚本：一次子进程同时返回 版本/pip/PyInstaller/PyArmor 状态，
// 把原先每环境 5~6 次 Python 冷启动压缩到 1 次（配合并行检测，整体提速数倍）。
const detectScript = `import sys, json
out = {}
try:
    out['version'] = sys.version.split()[0]
except Exception:
    out['version'] = ''
try:
    import pip
    out['pip'] = True
except Exception:
    out['pip'] = False
try:
    import PyInstaller
    out['pyi'] = True
except Exception:
    out['pyi'] = False
try:
    import sysconfig, os
    sp = sysconfig.get_path('scripts')
    out['pyarmor_exe'] = os.path.join(sp, 'pyarmor.exe' if os.name == 'nt' else 'pyarmor')
    out['pyarmor'] = os.path.isfile(out['pyarmor_exe'])
except Exception:
    out['pyarmor'] = False
    out['pyarmor_exe'] = ''
print(json.dumps(out))
`

// Search 自动搜索所有可用的 Python 环境
// 策略：
//  1. Windows Python Launcher 注册的安装（py -0p）
//  2. 系统 PATH 中的 python
//  3. 常见 Python 安装目录
//  4. 用户自定义环境根目录下的虚拟环境（含 pyvenv.cfg）
func Search(customRoot string) ([]config.PythonEnv, error) {
	seen := map[string]bool{}
	envs := []config.PythonEnv{}

	add := func(p string, isVirtual bool, root string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		envs = append(envs, config.PythonEnv{
			Path:      abs,
			IsVirtual: isVirtual,
			Root:      root,
		})
	}

	// 1. py launcher（Windows）
	for _, p := range pyLauncherEnvs() {
		add(p, false, "")
	}

	// 2. PATH 中的 python
	for _, p := range scanPath() {
		add(p, false, "")
	}

	// 3. 常见安装目录
	for _, p := range scanCommonDirs() {
		add(p, false, "")
	}

	// 3.5 整个 C 盘兜底扫描（最多 4 层，跳过系统/缓存大目录；装在任何位置的 Python 也能被发现）
	for _, p := range scanDriveC() {
		add(p, false, "")
	}

	// 4. 自定义根目录下的虚拟环境
	if customRoot != "" {
		if vs := scanVirtualEnvs(customRoot); len(vs) > 0 {
			for _, v := range vs {
				add(v.python, true, v.root)
			}
		}
	}

	// 逐个补全检测信息（并发执行 + 30 秒短时缓存）；无法识别版本的环境（如 Windows 应用执行别名/快捷方式代理）直接剔除
	sem := make(chan struct{}, 6) // 并发上限 6，避免同时启动过多 Python 进程卡死机器
	var wg sync.WaitGroup
	for i := range envs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, err := Detect(envs[i].Path)
			if err != nil || d.Version == "" {
				return // 检测失败：在收尾阶段剔除
			}
			envs[i].Version = d.Version
			envs[i].HasPip = d.HasPip
			envs[i].HasPyInstaller = d.HasPyInstaller
			envs[i].HasPyarmor = d.HasPyarmor // 此前漏拷贝，导致首页 PyArmor 状态恒为"未安装"
			envs[i].IsVirtual = d.IsVirtual
			envs[i].Root = d.Root
		}(i)
	}
	wg.Wait()
	kept := envs[:0]
	for _, e := range envs {
		if e.Version == "" {
			continue
		}
		kept = append(kept, e)
	}
	envs = kept

	// 命名 + 排序
	for i := range envs {
		envs[i].Name = displayName(&envs[i])
	}
	sort.Slice(envs, func(a, b int) bool {
		return strings.ToLower(envs[a].Name) < strings.ToLower(envs[b].Name)
	})
	return envs, nil
}

// Detect 检测单个 Python 环境的版本 / pip / PyInstaller / PyArmor
// 带 30 秒短时缓存：同一路径短时间内重复调用直接返回上次结果。
func Detect(pythonPath string) (config.PythonEnv, error) {
	env := config.PythonEnv{Path: pythonPath}
	if pythonPath == "" {
		return env, os.ErrNotExist
	}

	// 命中短时缓存直接返回（避免切换页面/重复搜索反复冷启动子进程）
	detectCacheMu.Lock()
	if c, ok := detectCache[pythonPath]; ok && time.Since(c.at) < detectCacheTTL*time.Second {
		detectCacheMu.Unlock()
		return c.env, c.err
	}
	detectCacheMu.Unlock()

	if _, err := os.Stat(pythonPath); err != nil {
		return env, err
	}
	// 合并检测：一次子进程返回 版本/pip/PyInstaller/PyArmor 四项
	env = detectMerged(pythonPath)
	// PyArmor 需额外验证可执行文件可用（仅安装了 pyarmor 的环境才多跑一次 --version）
	if env.HasPyarmor {
		env.HasPyarmor = hasPyarmor(pythonPath)
	}
	// 虚拟环境识别：python.exe 的上级目录（Scripts/bin）的上一级存在 pyvenv.cfg 即为虚拟环境
	if root, ok := venvRootOf(pythonPath); ok {
		env.IsVirtual = true
		env.Root = root
	}

	detectCacheMu.Lock()
	detectCache[pythonPath] = detectCacheItem{at: time.Now(), env: env}
	detectCacheMu.Unlock()
	return env, nil
}

// detectMerged 用合并脚本一次子进程检测版本 / pip / PyInstaller / PyArmor（替代原先 5~6 次进程）
func detectMerged(pythonPath string) config.PythonEnv {
	env := config.PythonEnv{Path: pythonPath}
	out, err := runQuiet(pythonPath, "-c", detectScript)
	if err != nil || out == "" {
		return env
	}
	var d struct {
		Version    string `json:"version"`
		Pip        bool   `json:"pip"`
		Pyi        bool   `json:"pyi"`
		Pyarmor    bool   `json:"pyarmor"`
		PyarmorExe string `json:"pyarmor_exe"`
	}
	if json.Unmarshal([]byte(out), &d) != nil {
		return env
	}
	env.Version = d.Version
	env.HasPip = d.Pip
	env.HasPyInstaller = d.Pyi
	env.HasPyarmor = d.Pyarmor
	return env
}

// venvRootOf 判断 python 可执行文件是否属于虚拟环境，返回其根目录（含 pyvenv.cfg 的目录）
func venvRootOf(pythonPath string) (string, bool) {
	dir := filepath.Dir(pythonPath) // ...\Scripts 或 ...\bin
	parent := filepath.Dir(dir)     // 虚拟环境根目录
	if fileExists(filepath.Join(parent, "pyvenv.cfg")) {
		return parent, true
	}
	return "", false
}

// FindPyarmor 定位所选 Python 环境中的 pyarmor 可执行文件（PyArmor 8+ 是独立 CLI，
// 安装于该环境的 scripts 目录，不能通过 "python -m pyarmor" 调用）。找不到返回空串。
func FindPyarmor(pythonPath string) string {
	if pythonPath == "" {
		return ""
	}
	// 通过 sysconfig 获取该 Python 环境真实的 scripts 目录（对 venv 与系统 Python 均准确）
	script := `import sysconfig,os; print(os.path.join(sysconfig.get_path('scripts'),'pyarmor.exe'))`
	out, err := runQuiet(pythonPath, "-c", script)
	if err != nil {
		return ""
	}
	if out == "" {
		return ""
	}
	if !fileExists(out) {
		return ""
	}
	return out
}

func hasPyarmor(pythonPath string) bool {
	exe := FindPyarmor(pythonPath)
	if exe == "" {
		return false
	}
	_, err := runQuiet(exe, "--version")
	return err == nil
}

// scanPath 从 PATH 环境变量中收集 python 可执行文件（过滤 Windows 应用执行别名等假环境）
func scanPath() []string {
	var out []string
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		for _, name := range pythonExecutables() {
			full := filepath.Join(dir, name)
			if fileExists(full) && !isFakePython(full) {
				out = append(out, full)
			}
		}
	}
	return out
}

// scanCommonDirs 扫描常见 Python 安装目录
func scanCommonDirs() []string {
	var out []string
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(paths.LocalAppData(), "Programs", "Python"))
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		if programFiles != "" {
			candidates = append(candidates, filepath.Join(programFiles, "Python"))
		}
		if programFilesX86 != "" {
			candidates = append(candidates, filepath.Join(programFilesX86, "Python"))
		}
	}
	for _, root := range candidates {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(root, e.Name(), pythonExeName())
			if fileExists(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// skipDriveDirs 全盘扫描时跳过的无关/超大目录名（大小写不敏感）：
// Windows 系统目录、已单独扫描的安装目录、各类缓存与依赖目录，
// 避免每次搜索都要遍历 C 盘海量文件导致卡顿。
var skipDriveDirs = map[string]bool{
	"windows":                true,
	"program files":          true,
	"program files (x86)":    true,
	"appdata":                true,
	"$recycle.bin":           true,
	"system volume information": true,
	"recovery":               true,
	"node_modules":           true,
	"site-packages":          true,
	".git":                   true,
	"__pycache__":            true,
	"build":                  true,
	"dist":                   true,
}

// scanDriveC 全盘扫描 C 盘：兜底收集任意位置的物理 Python（python.exe）
// 与虚拟环境（目录含 pyvenv.cfg，自动取 Scripts/bin 下的 python.exe）。
// 最多深入 4 层（与虚拟环境扫描一致），并跳过系统与缓存大目录
// （skipDriveDirs）以及点开头的隐藏缓存目录（.cache/.gradle 等；
// 真正的虚拟环境 .venv 含 pyvenv.cfg，会先被识别并记录）。
// 非 Windows 平台返回空。虚拟环境的 IsVirtual 标记由后续 Detect 自动补全。
func scanDriveC() []string {
	var out []string
	if runtime.GOOS != "windows" {
		return out
	}
	root := "C:\\"
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return out
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 无权限/被占用的目录直接跳过
		}
		if d.IsDir() {
			if path != root {
				// 深度限制：相对 C:\ 最多深入 4 层，避免遍历过深过慢
				rel, _ := filepath.Rel(root, path)
				if strings.Count(rel, string(filepath.Separator)) >= 4 {
					return filepath.SkipDir
				}
				// 虚拟环境优先识别（.venv 等点开头目录也走这里），识别后不再深入
				if fileExists(filepath.Join(path, "pyvenv.cfg")) {
					py := filepath.Join(path, "Scripts", pythonExeName())
					if !fileExists(py) {
						py = filepath.Join(path, "bin", pythonExeName())
					}
					if fileExists(py) {
						out = append(out, py)
					}
					return filepath.SkipDir
				}
				name := strings.ToLower(d.Name())
				if strings.HasPrefix(name, ".") || skipDriveDirs[name] {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.EqualFold(d.Name(), pythonExeName()) {
			out = append(out, path)
		}
		return nil
	})
	return out
}

type venvFound struct {
	root   string
	python string
}

// scanVirtualEnvs 在指定根目录下递归查找虚拟环境（含 pyvenv.cfg）
func scanVirtualEnvs(root string) []venvFound {
	var out []venvFound
	root = filepath.Clean(root)
	if _, err := os.Stat(root); err != nil {
		return out
	}
	maxDepth := 4
	walkFn := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			// 虚拟环境标志：pyvenv.cfg
			cfg := filepath.Join(path, "pyvenv.cfg")
			if fileExists(cfg) {
				py := filepath.Join(path, "Scripts", pythonExeName())
				if !fileExists(py) {
					py = filepath.Join(path, "bin", pythonExeName())
				}
				if fileExists(py) {
					out = append(out, venvFound{root: path, python: py})
				}
				return filepath.SkipDir
			}
			// 跳过常见的无关大目录，加速扫描
			switch d.Name() {
			case "site-packages", "node_modules", ".git", "__pycache__":
				return filepath.SkipDir
			}
		}
		return nil
	}
	_ = filepath.WalkDir(root, walkFn)
	return out
}

func pythonExecutables() []string {
	if runtime.GOOS == "windows" {
		return []string{"python.exe", "python3.exe"}
	}
	return []string{"python3", "python"}
}

func pythonExeName() string {
	if runtime.GOOS == "windows" {
		return "python.exe"
	}
	return "python3"
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// isFakePython 识别"假"的 Python 环境：
//   - Windows 应用执行别名代理（位于 WindowsApps 目录，实为 0 字节/重解析点的快捷方式）
//   - 0 字节占位文件
func isFakePython(p string) bool {
	if strings.Contains(strings.ToLower(p), "windowsapps") {
		return true
	}
	if info, err := os.Stat(p); err == nil && info.Size() == 0 {
		return true
	}
	return false
}

// DisplayName 生成环境的展示名称：带物理/虚拟/内置便携版标识。
// 虚拟环境「虚拟环境 · venv · 根目录 · 版本」；内置便携版「内置便携版 · Python 版本 · 路径」；物理环境「物理环境 · Python 版本 · 路径」
func DisplayName(env *config.PythonEnv) string {
	return displayName(env)
}

func displayName(env *config.PythonEnv) string {
	// 内置便携版：标识 + Python + 版本 + 路径（打包预览等处直接展示）
	if env.IsPortable {
		name := "内置便携版 · Python"
		if env.Version != "" {
			name += " " + env.Version
		}
		if env.Path != "" {
			name += " · " + env.Path
		}
		return name
	}
	// 虚拟环境：标识 + venv + 完整根目录 + 版本（完整路径便于区分同名目录）
	if env.IsVirtual {
		name := "虚拟环境 · venv · " + env.Root
		if env.Version != "" {
			name += " · " + env.Version
		}
		return name
	}
	// 物理环境：标识 + Python + 版本 + 路径
	name := "物理环境 · Python"
	if env.Version != "" {
		name += " " + env.Version
	}
	if env.Path != "" {
		name += " · " + env.Path
	}
	return name
}

// ---- 命令行封装（便于测试/复用） ----
func runQuiet(name string, args ...string) (string, error) {
	cmd := cmdutil.Command(name, args...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
