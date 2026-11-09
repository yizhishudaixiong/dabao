package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/deps"
	"pypacker/internal/fsx"
)

// BuildArgs 根据配置组装 PyInstaller 参数（基于 python -m PyInstaller 调用）
// entryScript: 实际入口脚本路径（未加密时为 cfg.EntryScript；启用加密时为 PyArmor 加密后的脚本）
// extraData:   额外的 --add-data 数据项（如 "src;dst"），用于把 pyarmor_runtime 运行时打入产物
// encProjRoot: 启用加密时为加密项目根目录（PyInstaller 从这里收集加密模块），未加密传空串
// onLog:       可选日志回调，用于加密模式下内置依赖分析的进度提示
func BuildArgs(cfg *config.BuildConfig, workspace, entryScript string, extraData []string, encProjRoot string, onLog func(string)) ([]string, error) {
	if entryScript == "" {
		return nil, fmt.Errorf("请先选择 Python 入口脚本")
	}
	if err := validateAppName(cfg.Name); err != nil {
		return nil, err
	}
	if cfg.PythonPath == "" {
		return nil, fmt.Errorf("请先选择 Python 环境")
	}

	// 打包缓存加速：默认开启 → 不传 --clean，保留 PyInstaller 全局编译缓存（bincache），重复打包更快；
	// 关闭 → 传 --clean 强制全新构建（升级依赖/换 Python 后打包异常时使用）
	args := []string{"-m", "PyInstaller", "--noconfirm"}
	if !cfg.CacheAccel {
		args = append(args, "--clean")
	}

	// 手动补充的隐藏导入（动态导入/插件式加载的模块，静态分析发现不了）
	for _, hi := range cfg.HiddenImports {
		hi = strings.TrimSpace(hi)
		if hi != "" {
			args = append(args, "--hidden-import", hi)
		}
	}

	// 单文件 / 多文件
	if cfg.OneFile {
		args = append(args, "--onefile")
	} else {
		args = append(args, "--onedir")
		// 多文件模式可自定义依赖文件夹名（默认 _internal）
		if cfg.ContentsDir != "" {
			if err := validateContentsDir(cfg.ContentsDir); err != nil {
				return nil, err
			}
			args = append(args, "--contents-directory", cfg.ContentsDir)
		}
	}

	// 是否有控制台窗口
	if cfg.NoConsole {
		args = append(args, "--windowed")
	} else {
		args = append(args, "--console")
	}

	// 管理员权限运行：打包出的程序启动时自动请求提权（UAC）
	if cfg.UacAdmin {
		args = append(args, "--uac-admin")
	}

	// 名称
	args = append(args, "--name", cfg.Name)

	// 图标（支持 .ico / .png / .jpg，非 ico 自动转换为 .ico）
	if cfg.IconPath != "" {
		iconArg := cfg.IconPath
		if strings.ToLower(filepath.Ext(cfg.IconPath)) != ".ico" {
			icoPath := filepath.Join(workspace, "app_icon.ico")
			if err := ConvertToIco(cfg.IconPath, icoPath); err != nil {
				return nil, fmt.Errorf("图标转换失败: %v", err)
			}
			iconArg = icoPath
		}
		args = append(args, "--icon", iconArg)
	}

	// 版本 / 作者等元数据 -> version-file
	if cfg.Version != "" || cfg.Author != "" {
		verFile := filepath.Join(workspace, "version_info.txt")
		if err := writeVersionFile(verFile, cfg); err != nil {
			return nil, fmt.Errorf("生成版本信息失败: %v", err)
		}
		args = append(args, "--version-file", verFile)
	}

	// 精简模式：排除一批用不到的标准库模块
	if cfg.LiteMode {
		args = append(args, liteExcludes()...)
	}

	// 用户手动排除的模块（--exclude-module）：只排除确信用不到的库，避免打包后缺模块
	for _, m := range cfg.ExcludeModules {
		m = strings.TrimSpace(m)
		if m != "" {
			args = append(args, "--exclude-module", m)
		}
	}

	// 资源一起打包（--add-data，Windows 使用 ; 分隔）。
	// 目标路径规则（与前端"打包后路径"显示保持一致）：
	// - 程序目录内的资源：保留相对入口脚本目录的层级（如 assets\logo.png 进包后仍是 assets\logo.png）
	// - 程序目录外的文件：放资源根（target=文件名）
	// - 程序目录外的文件夹：保留文件夹本身及内部结构（target=文件夹名）
	projDir := ""
	if cfg.EntryScript != "" {
		projDir = filepath.Dir(cfg.EntryScript)
	}
	for _, r := range cfg.Resources {
		if !r.Enabled || r.Path == "" {
			continue
		}
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return nil, fmt.Errorf("资源路径无效: %s", r.Path)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("资源不存在: %s", r.Path)
		}
		target := resourceTarget(abs, projDir, r.IsDir)
		args = append(args, "--add-data", abs+";"+target)
	}

	// 使用内置 UPX 压缩产物（体积更小，但可能提高杀软误报率；工具自带 UPX，无需联网下载）
	if cfg.UseUPX {
		upxDir, err := EnsureUPX()
		if err != nil {
			return nil, fmt.Errorf("UPX 准备失败: %v", err)
		}
		args = append(args, "--upx-dir", upxDir)
	}

	// 附加参数
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	}

	// 把当前 Python 环境里已安装的所有发行版元数据（dist-info）复制进打包产物。
	// 原因：部分第三方包（streamlit、altair 等）导入时会通过 importlib.metadata
	// 查询自己的版本/依赖信息，PyInstaller 默认不收集 dist-info，打包后运行会报
	// PackageNotFoundError。与其逐个枚举"哪些包需要"，不如全部复制
	// （每个 dist-info 仅几十 KB 文本），任何包需要元数据都能查到，不会漏。
	args = appendCopyMetadataArgs(cfg, args)

	// 项目目录加入模块搜索路径，并把本地 .py 模块 / 子包作为 --hidden-import 打入，
	// 解决 PyInstaller 对非 ASCII 模块名（如 "配置.py"）的 import 无法自动收集的缺陷，
	// 否则打包后运行会报 "No module named '配置'"。
	// - 未加密：基于原始入口脚本 cfg.EntryScript 收集（目录=脚本所在目录）
	// - 加密后：所有自写模块都被 PyArmor 加密，必须从加密项目根 encProjRoot 收集，
	//   否则进包的是明文模块；入口同名模块名不变，排除逻辑一致。
	if cfg.EntryScript != "" {
		var modDir, entryBase string
		encrypted := encProjRoot != ""
		if encrypted {
			modDir = encProjRoot
			entryBase = strings.TrimSuffix(filepath.Base(entryScript), filepath.Ext(entryScript))
		} else {
			modDir = filepath.Dir(cfg.EntryScript)
			entryBase = strings.TrimSuffix(filepath.Base(cfg.EntryScript), filepath.Ext(cfg.EntryScript))
		}
		args = append(args, "--paths", modDir)
		if encrypted {
			// 加密后 PyInstaller 从任何加密模块里都看不到 import 语句，整个导入图是断的：
			// 只补顶层模块的话，`from pkg import widgets` 会因为 pkg.widgets 没被收集而在
			// 运行时报 ImportError。必须把项目内所有本地模块（含子包里的每个模块）逐个补上，
			// 等价于 PyArmor 官方 --pack 用明文分析结果替换 a.pure 路径的做法。
			for _, m := range collectLocalModules(modDir, entryBase) {
				args = append(args, "--hidden-import", m)
			}
		} else {
			// 未加密时 PyInstaller 能顺着真实 import 图收集，只需兜底顶层模块
			// （主要针对非 ASCII 模块名，PyInstaller 对其收集有缺陷）
			for _, m := range collectModulesIn(modDir, entryBase) {
				args = append(args, "--hidden-import", m)
			}
		}
	}

	// 加密运行时目录作为数据一并打入（PyArmor 加密脚本运行时需要它）
	for _, d := range extraData {
		args = append(args, "--add-data", d)
	}

	// 加密模式下 PyInstaller 从加密入口的 AST 中看不到真实的 import 语句
	// （真正的 import 被 PyArmor 藏进加密的 code 对象里），第三方依赖会全部漏收集，
	// 表现为产物体积缩水 10 倍以上且运行报 "No module named xxx"。
	// 因此加密时必须用 PyInstaller 官方分析引擎（与正式打包同一套：字节码扫描 +
	// 上千官方钩子 + 递归依赖树）先分析明文入口，把收集到的第三方模块清单喂给打包：
	//   1) --hidden-import <顶层第三方模块名>：来自官方引擎收集清单（含钩子补充的
	//      动态导入模块），逐个激活收集；解决 pywin32 这类"模块不在包目录内"的特例
	//      （win32gui.pyd 在 win32/ 目录，--collect-all pywin32 收集不到，必须
	//      --hidden-import win32gui 激活钩子）
	//   2) --collect-all <pip包名>：完整收集包目录下的所有子模块 + 数据 + 二进制
	//      （PySide6 有几十个 .pyd 子模块，只 hidden-import 顶层会缺 QtWidgets 等）
	// PyInstaller / PyArmor 是打包工具本身，绝不能打进用户产物，必须排除。
	// 官方分析引擎有缓存：依赖页分析过则此处秒回，不会重复耗时。
	if encProjRoot != "" {
		// 加密打包前必须先用官方分析引擎收集第三方模块清单；缓存命中秒回，
		// 首次（用户未在依赖页分析过）约 5~60 秒，此时向构建日志推送进度提示，避免误以为卡死
		if onLog != nil {
			onLog("正在分析依赖（首次约 5~60 秒，已分析过则秒回）…")
		}
		res, err := deps.Analyze(cfg.EntryScript, cfg.PythonPath, func(line string) {
			if onLog != nil {
				onLog(line)
			}
		})
		if err != nil {
			return nil, fmt.Errorf("加密打包需要先分析依赖：%v", err)
		}
		// 1) 官方引擎收集的第三方模块顶层名，逐个 hidden-import（激活对应钩子）
		topSeen := map[string]bool{}
		for _, mod := range res.AnalysisModules {
			top := strings.SplitN(mod, ".", 2)[0]
			if top == "" || topSeen[top] {
				continue
			}
			// 用户明确排除的模块不能再补回来，否则「排除模块」在加密模式下完全失效
			if excludedModule(cfg.ExcludeModules, top) {
				continue
			}
			topSeen[top] = true
			args = append(args, "--hidden-import", top)
		}
		// 2) 已装/缺失/钩子补充的包名，逐个 collect-all（全量收集子模块与数据）
		seen := map[string]bool{}
		for _, d := range append(append(res.Resolved, res.Missing...), res.HookExtra...) {
			pkg := strings.TrimSpace(d.PkgName)
			if pkg == "" || seen[pkg] || isToolPkg(pkg) {
				continue
			}
			// 整个包都被用户排除时不要再 collect-all：
			// --exclude-module 只挡模块，collect-all 带进来的数据/二进制仍会被打进产物
			if allModulesExcluded(d.ImportName, cfg.ExcludeModules) {
				continue
			}
			seen[pkg] = true
			args = append(args, "--collect-all", pkg)
		}
	}

	// 命令行长度兜底：Windows 单条命令行上限 32767 字符（CreateProcess），
	// 大环境（装了几百个包）时 --copy-metadata + --collect-all + --hidden-import
	// 很容易逼近这个长度，超了会直接启动失败，且报错是难以理解的
	// "The filename or extension is too long"。先丢掉最不关键的 --copy-metadata，再超就明确报错。
	args = fitCmdline(args, onLog)
	if n := cmdlineLen(args); n > maxCmdlineLen {
		return nil, fmt.Errorf("打包参数过长（约 %d 字符，Windows 命令行上限 32767）：当前环境安装的包太多。建议在多文件模式下打包，或减少依赖数量", n)
	}

	// 入口脚本（未加密时为用户脚本；加密时传入加密后的脚本）
	absEntry, err := filepath.Abs(entryScript)
	if err != nil {
		return nil, fmt.Errorf("入口脚本路径无效: %s", entryScript)
	}
	args = append(args, absEntry)

	return args, nil
}

// excludedModule 判断模块名是否被用户列入「排除模块」（大小写不敏感）。
//
// 匹配方向很重要，只能"从上往下"：排除 pkg 应挡住 pkg.sub；
// 但排除 pkg.sub 不能挡住 pkg —— PyInstaller 的 --exclude-module pkg.sub
// 只移除那一个子模块，父包其余部分仍然要用。如果因为排除了一个子模块就跳过
// 整个包的 hidden-import / collect-all，加密产物里整包都会缺。
func excludedModule(excludes []string, mod string) bool {
	mod = strings.ToLower(strings.TrimSpace(mod))
	if mod == "" {
		return false
	}
	for _, e := range excludes {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if mod == e || strings.HasPrefix(mod, e+".") {
			return true
		}
	}
	return false
}

// allModulesExcluded 判断一条依赖的全部 import 名是否都被排除。
// ImportName 形如 "win32api, win32gui"；只有全部命中才跳过 collect-all，
// 部分命中时仍要收集（否则剩下的模块会缺）。
func allModulesExcluded(importNames string, excludes []string) bool {
	mods := strings.Split(importNames, ",")
	found := false
	for _, m := range mods {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		found = true
		if !excludedModule(excludes, m) {
			return false
		}
	}
	return found
}

// maxCmdlineLen Windows 命令行安全上限（系统上限 32767，留出余量）
const maxCmdlineLen = 30000

// cmdlineLen 估算拼接后的命令行长度（参数 + 分隔空格）
func cmdlineLen(args []string) int {
	n := 0
	for _, a := range args {
		n += len(a) + 1
	}
	return n
}

// fitCmdline 在命令行过长时按"最不影响产物"的顺序裁剪：
//  1. 丢掉全部 --copy-metadata（几百个包时占 1 万字符以上；缺元数据只会让
//     少数包在查询自身版本时报 PackageNotFoundError）
//  2. 仍然超长就原样返回，由调用方报错（不擅自删 --hidden-import / --collect-all，
//     删掉它们会直接导致产物缺模块）
func fitCmdline(args []string, onLog func(string)) []string {
	if cmdlineLen(args) <= maxCmdlineLen {
		return args
	}
	out := make([]string, 0, len(args))
	dropped := 0
	for i := 0; i < len(args); i++ {
		if args[i] == "--copy-metadata" && i+1 < len(args) {
			i++
			dropped++
			continue
		}
		out = append(out, args[i])
	}
	if dropped > 0 && onLog != nil {
		onLog(fmt.Sprintf("[警告] 打包参数过长，已跳过 %d 条 --copy-metadata（不影响程序功能；若某库提示找不到自身版本信息，可在「附加参数」里手动补 --copy-metadata 包名）", dropped))
	}
	return out
}

// appendCopyMetadataArgs 把 Python 环境里所有已安装发行版的元数据（dist-info）
// 逐个以 --copy-metadata 形式追加到 PyInstaller 参数里。
// 环境未就绪或查询失败时静默跳过（不阻塞打包），已存在的同名参数不重复追加。
func appendCopyMetadataArgs(cfg *config.BuildConfig, args []string) []string {
	if cfg.PythonPath == "" {
		return args
	}
	// 遍历 importlib.metadata 的全部发行版，输出各自的标准发行版名
	code := "import importlib.metadata as m\n" +
		"for d in m.distributions():\n" +
		"    n = d.metadata.get('Name') or ''\n" +
		"    if n:\n" +
		"        print(n)\n"
	out, err := cmdutil.Command(cfg.PythonPath, "-c", code).Output()
	if err != nil {
		return args
	}
	// 已有 --copy-metadata 的目标（如用户在附加参数里手动填过）不重复加
	existing := map[string]bool{}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--copy-metadata" {
			existing[args[i+1]] = true
			i++
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p := strings.TrimSpace(line)
		if p != "" && !existing[p] {
			args = append(args, "--copy-metadata", p)
		}
	}
	return args
}

// collectModulesIn 收集指定目录（加密产物目录或原项目目录）下的所有本地 .py 模块名和子包名，
// 返回给 PyInstaller 作 --hidden-import 使用。
// - .py 文件：取文件名（去掉 .py 后缀）作为模块名
// - 子目录：仅当含 __init__.py 时视为包
// 排除入口脚本自身、__pycache__、build/dist 等目录。
func collectModulesIn(dir, entryBase string) []string {
	var mods []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return mods
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// 子包目录（含 __init__.py）
			if _, err := os.Stat(filepath.Join(dir, name, "__init__.py")); err == nil {
				if name != "__pycache__" {
					mods = append(mods, name)
				}
			}
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".py") {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == entryBase {
			continue // 入口脚本本身
		}
		mods = append(mods, base)
	}
	return mods
}

// collectLocalModules 递归收集目录下所有本地模块的完整点分名（pkg、pkg.widgets、pkg.sub.mod），
// 专供「加密打包」使用。
//
// 为什么必须递归：PyArmor 加密后，PyInstaller 从加密脚本里读不到任何 import 语句，
// 整个导入图在加密模块处就断了。只补顶层模块（如 pkg）时，PyInstaller 会去分析
// encProjRoot/pkg/__init__.py，那同样是加密文件、同样没有 import，于是 pkg.widgets
// 永远不会被收集，产物运行报 `ImportError: cannot import name 'widgets' from 'pkg'`。
//
// 进入子目录的条件与 Python 的常规包语义一致：目录下有 __init__.py。
// 这样 tests/、docs/ 这类没有 __init__.py 的普通文件夹不会被当成包拖进产物。
func collectLocalModules(root, entryBase string) []string {
	var mods []string
	var walk func(dir, prefix string)
	walk = func(dir, prefix string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				if fsx.IsNoiseDir(name) || fsx.HasPyvenvCfg(filepath.Join(dir, name)) {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, name, "__init__.py")); err != nil {
					continue // 不是常规包
				}
				mods = append(mods, joinMod(prefix, name))
				walk(filepath.Join(dir, name), joinMod(prefix, name))
				continue
			}
			if !strings.EqualFold(filepath.Ext(name), ".py") {
				continue
			}
			base := strings.TrimSuffix(name, filepath.Ext(name))
			if base == "__init__" {
				continue // 包名本身已在进入目录时记录
			}
			// 只有合法标识符才能作为模块名导入（中文模块名合法，my-script 不合法）
			if !isModuleIdent(base) {
				continue
			}
			if prefix == "" && base == entryBase {
				continue // 入口脚本由 PyInstaller 直接处理
			}
			mods = append(mods, joinMod(prefix, base))
		}
	}
	walk(root, "")
	return mods
}

// isModuleIdent 判断字符串能否作为 Python 模块名导入（str.isidentifier 的 Go 侧近似实现）。
// 允许字母（含中文）、数字、下划线，且不能以数字开头。
func isModuleIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// joinMod 拼点分模块名（prefix 为空时返回 name 本身）
func joinMod(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// isToolPkg 判断是否为打包工具自身的包（PyInstaller / PyArmor 及其核心依赖），
// 这类包不能作为 --hidden-import 打进用户产物，否则体积暴涨且无意义
func isToolPkg(pkg string) bool {
	p := strings.ToLower(strings.TrimSpace(pkg))
	switch p {
	case "pyinstaller", "pyinstaller-hooks-contrib", "pyarmor", "pyarmor.cli",
		"pyarmor.cli.core", "pyarmor.cli.themida", "pyarmor-runtime":
		return true
	}
	return strings.HasPrefix(p, "pyinstaller") || strings.HasPrefix(p, "pyarmor")
}

// validateAppName 校验程序名称能否安全地作为 Windows 文件名使用。
// 该名称会参与 PyInstaller 的 --name、产物文件名，以及输出目录的路径拼接
// （filepath.Join(outputDir, cfg.Name+".exe")），含路径分隔符或 ".." 时
// 产物可能被写到用户选择的输出目录之外，因此必须在打包前挡掉。
func validateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("请填写程序名称")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("程序名称首尾不能包含空格")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("程序名称无效：%q", name)
	}
	if strings.ContainsAny(name, `\/:*?"<>|`) {
		return fmt.Errorf("程序名称不能包含 \\ / : * ? \" < > | 这些字符")
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("程序名称不能包含控制字符")
		}
	}
	// Windows 保留设备名不能作为文件名（CON.txt 同样不允许）
	switch strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name))) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("程序名称不能使用 Windows 保留名：%s", name)
	}
	return nil
}

// validateContentsDir 校验多文件模式的自定义依赖文件夹名称：
// 不能为空、不能为 . / ..、不能包含 Windows 非法字符
func validateContentsDir(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return fmt.Errorf("依赖文件夹名称无效：%q", name)
	}
	if strings.ContainsAny(trimmed, `\/:*?"<>|`) {
		return fmt.Errorf("依赖文件夹名称包含非法字符：%q（不能包含 \\ / : * ? \" < > |）", trimmed)
	}
	return nil
}

// liteExcludes 精简模式下排除的常见冗余模块
func liteExcludes() []string {
	return []string{
		"--exclude-module", "tkinter",
		"--exclude-module", "pydoc",
		"--exclude-module", "pydoc_data",
		"--exclude-module", "unittest",
		"--exclude-module", "doctest",
		"--exclude-module", "test",
		"--exclude-module", "lib2to3",
		"--exclude-module", "ensurepip",
		"--exclude-module", "turtledemo",
	}
}

// writeVersionFile 生成 PyInstaller 的 version-file 内容
// 版本号统一补零为四段（如 3.0 -> 3.0.0.0），保证 Windows 详情页
// 「文件版本」（读内部四段版本号）与「产品版本」（读字符串）显示一致；
// 作者同时写入「公司」与「版权」两栏，方便用户在文件属性中看到。
func writeVersionFile(path string, cfg *config.BuildConfig) error {
	ver := "1.0.0"
	if cfg.Version != "" {
		ver = cfg.Version
	}
	author := cfg.Author
	if author == "" {
		author = cfg.Name
	}
	// 将 "3.0" / "1.2.3" 统一为四段数字 [3,0,0,0] / [1,2,3,0]
	parts := strings.Split(ver, ".")
	v := [4]int{0, 0, 0, 0}
	for i, p := range parts {
		if i >= 4 {
			break
		}
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
			v[i] = n
		}
	}
	// 四段字符串，用于 StringFileInfo 的 FileVersion / ProductVersion
	verFull := fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
	content := fmt.Sprintf(`VSVersionInfo(
  ffi=FixedFileInfo(
    filevers=(%d, %d, %d, %d),
    prodvers=(%d, %d, %d, %d),
    mask=0x3f,
    flags=0x0,
    OS=0x40004,
    fileType=0x1,
    subtype=0x0,
    date=(0, 0)
  ),
  kids=[
    StringFileInfo([
      StringTable(
        '040904B0',
        [StringStruct('CompanyName', %q),
         StringStruct('FileDescription', %q),
         StringStruct('FileVersion', %q),
         StringStruct('InternalName', %q),
         StringStruct('LegalCopyright', %q),
         StringStruct('OriginalFilename', %q),
         StringStruct('ProductName', %q),
         StringStruct('ProductVersion', %q)])
    ]),
    VarFileInfo([VarStruct('Translation', [1033, 1200])])
  ]
)
`,
		v[0], v[1], v[2], v[3],
		v[0], v[1], v[2], v[3],
		author, cfg.Name, verFull, cfg.Name, "© "+author, cfg.Name+".exe", cfg.Name, verFull,
	)
	return os.WriteFile(path, []byte(content), 0o644)
}
