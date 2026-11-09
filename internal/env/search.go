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
	"pypacker/internal/fsx"
	"pypacker/internal/paths"
)

// detectCache 环境检测短时缓存：同一 Python 环境 30 秒内重复检测直接复用结果，
// 避免切换页面/重复搜索时反复启动子进程（每次检测 5~6 次进程冷启动非常慢）。
var (
	detectCache   = map[string]detectCacheItem{}
	detectCacheMu sync.Mutex
)

type detectCacheItem struct {
	at  time.Time
	env config.PythonEnv
	err error
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

// Search 快速搜索可用的 Python 环境：只查"确定可能出现 Python"的位置，
// 通常 1 秒内返回（耗时主要在给每个环境起 Python 子进程读版本），绝不遍历整个磁盘。环境是随时会变的（新装、卸载、移动），
// 所以这里不做任何跨次缓存，每次调用都是实时探测。
// 策略：
//  1. Windows Python Launcher 注册的安装（py -0p）
//  2. 系统 PATH 中的 python
//  3. 常见 Python 安装目录
//  4. 用户自定义环境根目录下的虚拟环境（含 pyvenv.cfg）
func Search(customRoot string) ([]config.PythonEnv, error) {
	return search(customRoot, false)
}

// SearchDeep 在 Search 的基础上再遍历整个 C 盘，捕捉装在任意位置的 Python。
// 遍历较慢（数秒到数十秒，取决于磁盘），只由用户点「深度扫描」时触发，
// 且每次都是实时扫描，结果不与历史结果混用。
func SearchDeep(customRoot string) ([]config.PythonEnv, error) {
	return search(customRoot, true)
}

func search(customRoot string, deep bool) ([]config.PythonEnv, error) {
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
		// 过滤豆包自带沙箱运行时（Doubao\User Data\sandbox_runtime 下的内部 Python），
		// 它可能经 PATH 进入候选，但不应作为用户可选环境
		if strings.Contains(strings.ToLower(abs), "sandbox_runtime") {
			return
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

	// 3.5 全盘兜底扫描（仅深度扫描时执行）：捕捉 conda、绿色版等非标准位置。
	// 默认搜索不碰磁盘遍历，避免每次点"快速搜索"都卡几秒。
	if deep {
		for _, p := range scanDriveC() {
			add(p, false, "")
		}
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

	// 只缓存"检测成功"的结果：检测失败（Python 被杀软拦了一次、磁盘瞬时占用、
	// 刚装完环境还没就绪等）如果也缓存 30 秒，该环境会在列表里凭空消失半分钟，
	// 表现为"明明装了却搜不到"。
	if env.Version != "" {
		detectCacheMu.Lock()
		detectCache[pythonPath] = detectCacheItem{at: time.Now(), env: env}
		detectCacheMu.Unlock()
	}
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

// scanCommonDirs 扫描常见 Python 安装目录。
// 除官方安装器默认位置外，还覆盖 Miniconda / Anaconda 的默认安装目录与 conda 环境目录 ——
// 这些是最常见的"非标准位置"，不覆盖的话用户只能靠「深度扫描」才能发现自己的环境。
func scanCommonDirs() []string {
	var out []string
	var versionRoots []string // 形如 ...\Python\Python312\python.exe（版本号子目录下）
	var directDirs []string   // 形如 ...\miniconda3\python.exe（目录本身即环境根）

	addDirect := func(dir string) {
		p := filepath.Join(dir, pythonExeName())
		if fileExists(p) && !isFakePython(p) {
			out = append(out, p)
		}
	}
	// 枚举 root 下的子目录（每个子目录是一个独立环境，如 .conda\envs\<名称>）
	addChildren := func(root string) {
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				addDirect(filepath.Join(root, e.Name()))
			}
		}
	}

	if runtime.GOOS == "windows" {
		versionRoots = append(versionRoots, filepath.Join(paths.LocalAppData(), "Programs", "Python"))
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			versionRoots = append(versionRoots, filepath.Join(programFiles, "Python"))
		}
		if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
			versionRoots = append(versionRoots, filepath.Join(programFilesX86, "Python"))
		}
		if home := os.Getenv("USERPROFILE"); home != "" {
			directDirs = append(directDirs,
				filepath.Join(home, "miniconda3"),
				filepath.Join(home, "anaconda3"),
				filepath.Join(home, "mambaforge"),
			)
			addChildren(filepath.Join(home, ".conda", "envs"))
		}
		if pd := os.Getenv("ProgramData"); pd != "" {
			directDirs = append(directDirs,
				filepath.Join(pd, "miniconda3"),
				filepath.Join(pd, "anaconda3"),
			)
			addChildren(filepath.Join(pd, "miniconda3", "envs"))
			addChildren(filepath.Join(pd, "anaconda3", "envs"))
		}
	}

	for _, root := range versionRoots {
		addChildren(root)
	}
	for _, dir := range directDirs {
		addDirect(dir)
	}
	return out
}

// driveMaxDepth 全盘扫描的最大深度（root 的直接子目录为第 1 层）。
// 7 层是实测的性价比拐点：能覆盖 Windows 默认的"仅为当前用户安装"位置
// （Users\名\AppData\Local\Programs\Python\Python3xx，第 7 层）以及 conda 环境等
// 非标准安装；继续放宽到 8/9 层耗时翻倍且几乎无新增命中。
// 同时这个深度已经能避开豆包沙箱运行时（约 9 层）等深层嵌套目录。
const driveMaxDepth = 7

// scanWorkers 全盘扫描的并发度：按顶层目录并行遍历（各子树互不重叠，无需去重）。
// 深度扫描本身较慢，并发能明显缩短等待时间，也不会给磁盘造成过大压力。
const scanWorkers = 6

// scanDriveC 全盘扫描 C 盘：兜底收集任意位置的物理 Python（python.exe）
// 与虚拟环境（目录含 pyvenv.cfg，自动取 Scripts/bin 下的 python.exe）。
// 相对 C:\ 最多深入 driveMaxDepth 层（含 Program Files / AppData 等位置），
// 并跳过系统与缓存大目录（skipDriveDirs）以及点开头的隐藏缓存目录
// （.cache/.gradle 等；真正的虚拟环境 .venv 含 pyvenv.cfg，会先被识别并记录）。
// 非 Windows 平台返回空。虚拟环境的 IsVirtual 标记由后续 Detect 自动补全。
// 注意：本函数只在用户主动「深度扫描」时调用，不做任何跨次缓存（环境会变）。
func scanDriveC() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	root := `C:\`
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}
	return scanTree(root)
}

// scanTree 遍历 root 下的全部目录，返回找到的 python.exe 路径。
// 与 filepath.WalkDir 相比有两点不同，目的是让全盘扫描尽量快：
//  1. 每个目录只 ReadDir 一次，直接用返回的条目判断 pyvenv.cfg / python.exe 是否存在，
//     省掉"每个目录再 Stat 一次标志文件"的额外系统调用（全盘约 1.4 万个目录，占比不小）；
//  2. 顶层目录并发遍历，充分利用多核与磁盘队列。
//
// 语义与原来保持一致：不跟随符号链接/junction（避免目录环），
// 目录名命中 skipDriveDirs 或点开头的跳过，但含 pyvenv.cfg 的目录会先被识别为虚拟环境。
func scanTree(root string) []string {
	return scanTreeDepth(root, driveMaxDepth)
}

// scanTreeDepth 同 scanTree，深度限制可注入（便于测试与评估不同深度的开销）。
func scanTreeDepth(root string, maxDepth int) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var (
		mu  sync.Mutex
		out []string
		wg  sync.WaitGroup
	)
	sem := make(chan struct{}, scanWorkers)
	collect := func(paths []string) {
		if len(paths) == 0 {
			return
		}
		mu.Lock()
		out = append(out, paths...)
		mu.Unlock()
	}
	for _, e := range entries {
		if e.IsDir() {
			if isLinkEntry(e) {
				continue
			}
			wg.Add(1)
			go func(dir string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				collect(scanDir(dir, 1, maxDepth))
			}(filepath.Join(root, e.Name()))
			continue
		}
		// 直接放在盘符根目录下的 python.exe（极少见，但与顺序遍历的实现等价）
		if strings.EqualFold(e.Name(), pythonExeName()) {
			if p := filepath.Join(root, e.Name()); !isFakePython(p) {
				collect([]string{p})
			}
		}
	}
	wg.Wait()
	return out
}

// scanDir 递归扫描单个目录。depth 为该目录相对 root 的层数（root 的直接子目录为 1），
// 超过 maxDepth 层不再深入。
func scanDir(dir string, depth, maxDepth int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 无权限 / 被占用：跳过
	}
	// 虚拟环境优先识别（放在跳过规则之前，保证 .venv 这类点开头目录也能被发现）：
	// 含 pyvenv.cfg 即为环境根，取 Scripts（Windows）/ bin（其它平台）下的 python.exe，
	// 且不再深入 —— venv 内部只有第三方库，继续扫没有意义。
	if hasEntry(entries, "pyvenv.cfg") {
		py := filepath.Join(dir, "Scripts", pythonExeName())
		if !fileExists(py) {
			py = filepath.Join(dir, "bin", pythonExeName())
		}
		if fileExists(py) {
			return []string{py}
		}
		return nil
	}
	name := strings.ToLower(filepath.Base(dir))
	// 跳过清单统一由 internal/fsx 维护（含 Windows 系统目录、回收站、各类缓存/依赖目录）。
	// Program Files / AppData 不在此列（里面也可能装 Python），深度由 driveMaxDepth 控制。
	if strings.HasPrefix(name, ".") || fsx.IsNoiseDir(name) || fsx.IsSystemJunkDir(name) {
		return nil
	}
	var out []string
	recurse := depth < maxDepth
	for _, e := range entries {
		if e.IsDir() {
			if !recurse || isLinkEntry(e) {
				continue
			}
			out = append(out, scanDir(filepath.Join(dir, e.Name()), depth+1, maxDepth)...)
			continue
		}
		if strings.EqualFold(e.Name(), pythonExeName()) {
			p := filepath.Join(dir, e.Name())
			if !isFakePython(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// isLinkEntry 是否为符号链接/junction：不跟随，避免目录环导致扫描失控
func isLinkEntry(e os.DirEntry) bool {
	return e.Type()&os.ModeSymlink != 0
}

// hasEntry 判断目录条目中是否存在指定文件（大小写不敏感，符合 Windows 语义）
func hasEntry(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), name) {
			return true
		}
	}
	return false
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
			// 跳过常见的无关大目录，加速扫描（清单与其它模块共用 internal/fsx）
			if fsx.IsNoiseDir(d.Name()) {
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
