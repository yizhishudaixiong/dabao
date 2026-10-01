package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pypacker/internal/builder"
	"pypacker/internal/config"
	"pypacker/internal/deps"
	"pypacker/internal/env"
	"pypacker/internal/paths"
	"pypacker/internal/portable"
	"pypacker/internal/version"
)

// App 是 Wails 暴露给前端的绑定对象
type App struct {
	ctx            context.Context
	mu             sync.Mutex
	cancelBuild    chan struct{}
	cancelDeps     chan struct{}      // 依赖安装取消通道（强制停止安装用）
	cancelPortable context.CancelFunc // 便携版下载取消（context 驱动，点取消立即中断连接）
	exitConfirmed  bool               // 用户已确认在忙碌状态下退出（避免重复弹确认框）
	activeTasks    int                // 统一忙碌计数：打包/安装/依赖分析/资源分析/便携版下载共用（IsBusy 只看它）
}

// markBusy / markIdle：所有长任务开始/结束时调用，统一驱动窗口关闭拦截。
// 新增任务时只需在这两处登记，无需再改 IsBusy。
func (a *App) markBusy() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeTasks++
}

func (a *App) markIdle() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeTasks > 0 {
		a.activeTasks--
	}
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// GetVersion 返回程序内部版本号（唯一来源：internal/version 包），供界面显示
func (a *App) GetVersion() string {
	return version.Version
}

// ---- 环境搜索与选择 ----

// SearchEnvironments 自动搜索所有可用 Python 环境（环境变量 + 自定义环境根目录下的虚拟环境），
// 并合并已下载的"内置便携版 Python"（带标识，可像普通环境一样使用）
func (a *App) SearchEnvironments(customRoot string) []config.PythonEnv {
	envs, err := env.Search(customRoot)
	if err != nil {
		envs = []config.PythonEnv{}
	}
	// 合并便携版：标记 IsPortable + 自带 pip（python-build-standalone 内置 pip 25.x）
	for _, p := range portable.List() {
		e := config.PythonEnv{
			Path:       p.Path,
			Version:    p.Version,
			HasPip:     true,
			IsPortable: true,
		}
		e.Name = "内置便携版 · Python " + p.Version + " · " + p.Path
		envs = append(envs, e)
	}
	return envs
}

// ---- 内置便携版 Python（无 Python 电脑也能打包）----

// GetPortableVersions 返回可下载的便携版 Python 版本清单
func (a *App) GetPortableVersions() []portable.VersionInfo {
	return portable.Versions()
}

// GetPortableRootDir 返回便携版存放目录（%LOCALAPPDATA%\dabao\runtime）
func (a *App) GetPortableRootDir() string {
	return portable.RootDir()
}

// GetPortablePythons 列出已下载的便携版（版本 / 路径 / 占用大小）
func (a *App) GetPortablePythons() []portable.PortablePython {
	return portable.List()
}

// DownloadPortablePython 开始异步下载指定版本的便携版 Python。
// 进度通过事件 portable:progress（含百分比/速度/阶段）推送，日志走 portable:log，
// 结束通过 portable:done 通知。同一时间只允许一个下载任务。
func (a *App) DownloadPortablePython(version string) error {
	a.mu.Lock()
	if a.cancelPortable != nil {
		a.cancelPortable()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelPortable = cancel
	a.mu.Unlock()

	go func() {
		a.markBusy()
		defer a.markIdle()
		defer func() {
			a.mu.Lock()
			a.cancelPortable = nil
			a.mu.Unlock()
		}()
		emit := func(evt string, data interface{}) {
			runtime.EventsEmit(a.ctx, evt, data)
		}
		dl := &portable.Downloader{
			OnLog: func(line string) { emit("portable:log", line) },
			OnDownload: func(done, total int64) {
				pct := 0
				if total > 0 {
					pct = int(done * 100 / total)
				}
				emit("portable:progress", map[string]interface{}{
					"percent":   pct,
					"doneMB":    float64(done) / 1024 / 1024,
					"totalMB":   float64(total) / 1024 / 1024,
					"speedMBps": 0, // 前端用两次进度差值算速度
					"stage":     "下载中",
				})
			},
			Ctx: ctx,
		}
		exe, err := portable.Download(version, dl)
		if err != nil {
			emit("portable:log", "下载失败："+err.Error())
			emit("portable:done", map[string]interface{}{"success": false, "message": err.Error(), "version": version})
			return
		}
		emit("portable:log", "便携版 Python 准备就绪："+exe)
		emit("portable:done", map[string]interface{}{"success": true, "message": "下载完成", "version": version})
	}()
	return nil
}

// CancelPortableDownload 立即终止正在进行的便携版下载（context 取消，所有连接即刻断开）
func (a *App) CancelPortableDownload() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelPortable != nil {
		a.cancelPortable()
		a.cancelPortable = nil
	}
}

// DeletePortablePython 删除指定版本的便携版整个文件夹（含已安装的依赖）
func (a *App) DeletePortablePython(version string) error {
	return portable.Delete(version)
}

// SelectCustomPython 弹出 Windows 文件选择框，让用户手动选择 python.exe
func (a *App) SelectCustomPython() (*config.PythonEnv, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 Python 可执行文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "Python 可执行文件", Pattern: "*.exe"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("打开文件选择框失败: %v", err)
	}
	if file == "" {
		return nil, fmt.Errorf("未选择文件")
	}
	e, err := env.Detect(file)
	if err != nil {
		return nil, fmt.Errorf("无法识别的 Python 环境: %v", err)
	}
	e.Name = env.DisplayName(&e)
	return &e, nil
}

// SelectEntryScript 选择打包入口 .py 文件
func (a *App) SelectEntryScript() (string, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 Python 入口脚本",
		Filters: []runtime.FileFilter{
			{DisplayName: "Python 脚本", Pattern: "*.py"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || file == "" {
		return "", err
	}
	return file, nil
}

// SelectIcon 选择程序图标（支持 .ico / .png / .jpg，非 ico 将自动转换）
func (a *App) SelectIcon() (string, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择程序图标（.ico / .png / .jpg）",
		Filters: []runtime.FileFilter{
			{DisplayName: "图标文件", Pattern: "*.ico;*.png;*.jpg;*.jpeg"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || file == "" {
		return "", err
	}
	return file, nil
}

// SelectOutputDir 选择最终 EXE 输出目录
func (a *App) SelectOutputDir() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择输出目录",
	})
	if err != nil || dir == "" {
		return "", err
	}
	return dir, nil
}

// SelectCustomRoot 弹出 Windows 文件夹选择框，选择自定义环境根目录
func (a *App) SelectCustomRoot() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择自定义环境根目录",
	})
	if err != nil || dir == "" {
		return "", err
	}
	return dir, nil
}

// SelectResourceDir 弹出 Windows 文件夹选择框，选择一个文件夹作为打包资源
func (a *App) SelectResourceDir() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要一起打包的文件夹",
	})
	if err != nil || dir == "" {
		return "", err
	}
	return dir, nil
}

// SelectResourceFile 弹出 Windows 文件选择框，选择一个文件作为额外打包资源
func (a *App) SelectResourceFile() (string, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要一起打包的文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || file == "" {
		return "", err
	}
	return file, nil
}

// AddResource 手动添加一个资源条目（文件夹或文件），供前端加入资源列表
func (a *App) AddResource(path string) (config.ResourceItem, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return config.ResourceItem{}, err
	}
	return config.ResourceItem{Path: path, Enabled: true, IsDir: fi.IsDir()}, nil
}

// AnalyzeResourceDeps 用指定 Python 执行 AST 扫描，分析代码引用的资源与已勾选资源的对比（资源依赖分析）
func (a *App) AnalyzeResourceDeps(pythonPath, entryScript string, resources []config.ResourceItem) ([]config.ResourceDep, error) {
	// 标记忙碌：窗口关闭拦截在资源分析期间也会弹确认框（IsBusy 看统一计数）
	a.markBusy()
	defer a.markIdle()
	return builder.AnalyzeResourceDeps(pythonPath, entryScript, resources)
}

// ---- 依赖分析与安装 ----

// AnalyzeDeps 分析入口脚本的第三方依赖，返回缺失项；
// 同时把构建必需依赖（PyInstaller）与加密必需依赖（选了加密时的 PyArmor）一并纳入结果，
// 缺失时前端会强制勾选安装，避免打包时才发现没装。
//
// 分析由 PyInstaller 官方分析引擎完成（字节码扫描 + 官方钩子），
// 分析引擎的实时日志通过 deps:analysis-log 事件推送给界面做进度显示。
// mirror 为自动补装 PyInstaller 时使用的 pip 源（与安装面板同源）。
func (a *App) AnalyzeDeps(entryScript, pythonPath, encryptMode, mirror string) (*config.DepsResult, error) {
	// 标记忙碌：窗口关闭拦截在分析期间也会弹确认框（IsBusy 看统一计数）
	a.markBusy()
	defer a.markIdle()
	// 分析引擎依赖目标环境装有 PyInstaller（与正式打包同一引擎）；
	// 全新环境未装时先自动补装，避免分析直接失败
	pi, pv := deps.CheckInstalled(pythonPath, "pyinstaller")
	if !pi {
		runtime.EventsEmit(a.ctx, "deps:analysis-log", "分析引擎需要 PyInstaller（打包必需组件），正在自动安装…")
		if err := deps.Install(pythonPath, []string{"pyinstaller"}, mirror, func(line string) {
			runtime.EventsEmit(a.ctx, "deps:analysis-log", line)
		}, nil, nil); err != nil {
			return nil, fmt.Errorf("自动安装 PyInstaller 失败：%v", err)
		}
		pi, pv = deps.CheckInstalled(pythonPath, "pyinstaller")
	}
	// 推送分析开始的阶段提示（首次分析约 5~15 秒，大项目 30~60 秒；重复分析秒回）
	runtime.EventsEmit(a.ctx, "deps:analysis-log", "正在执行 PyInstaller 官方分析引擎…")
	runtime.EventsEmit(a.ctx, "deps:progress", map[string]interface{}{"percent": 0, "stage": "启动分析引擎"})
	r, err := deps.Analyze(entryScript, pythonPath, func(line string) {
		runtime.EventsEmit(a.ctx, "deps:analysis-log", line)
	})
	if err != nil {
		return nil, err
	}
	runtime.EventsEmit(a.ctx, "deps:progress", map[string]interface{}{"percent": 100, "stage": "分析完成"})
	// 必需依赖：PyInstaller 始终检查
	mergeRequired(r, "pyinstaller", "pyinstaller", pi, pv)
	// 用户选了代码加密时，PyArmor 也强制纳入
	if encryptMode != "" && encryptMode != "none" {
		ok, ver := deps.CheckPyarmor(pythonPath)
		mergeRequired(r, "pyarmor", "pyarmor", ok, ver)
	}
	return r, nil
}

// mergeRequired 把必需依赖合并进分析结果：已存在同名项则标记 Required，
// 不存在则新增（缺失进 Missing，已装进 Resolved）
func mergeRequired(r *config.DepsResult, importName, pkgName string, installed bool, ver string) {
	for i := range r.Missing {
		if r.Missing[i].PkgName == pkgName {
			r.Missing[i].Required = true
			return
		}
	}
	for i := range r.Resolved {
		if r.Resolved[i].PkgName == pkgName {
			r.Resolved[i].Required = true
			return
		}
	}
	item := config.DepInfo{
		ImportName:       importName,
		PkgName:          pkgName,
		InstalledVersion: ver,
		Installed:        installed,
		Required:         true,
	}
	if installed {
		r.Resolved = append(r.Resolved, item)
	} else {
		r.Missing = append(r.Missing, item)
	}
}

// InstallDeps 将缺失依赖安装到用户所选 Python 环境（实时推日志 + 进度）。
// mirror 可选：空=官方源，或传入清华/阿里等镜像 URL（如 "https://pypi.tuna.tsinghua.edu.cn/simple"）
func (a *App) InstallDeps(pythonPath string, pkgs []string, mirror string) error {
	ch := a.beginDeps()
	// 安装结束后（无论成败）清理取消通道，避免窗口关闭拦截误判"正在安装"
	defer func() {
		a.markIdle()
		a.mu.Lock()
		if a.cancelDeps == ch {
			a.cancelDeps = nil
		}
		a.mu.Unlock()
	}()
	a.markBusy()
	return deps.Install(pythonPath, pkgs, mirror, func(line string) {
		runtime.EventsEmit(a.ctx, "deps:log", line)
	}, a.emitDepsProgress, ch)
}

// beginDeps 创建/重置依赖安装取消通道（同一时间只允许一个安装任务）
func (a *App) beginDeps() chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelDeps != nil {
		close(a.cancelDeps)
	}
	ch := make(chan struct{})
	a.cancelDeps = ch
	return ch
}

// CancelDepsInstall 强制停止正在进行的依赖安装（用户点击「强制停止」时调用）
func (a *App) CancelDepsInstall() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelDeps != nil {
		close(a.cancelDeps)
		a.cancelDeps = nil
	}
}

// IsBusy 是否有任务正在执行（打包/安装/依赖分析/资源分析/便携版下载），供窗口关闭拦截使用。
// 统一看 activeTasks 计数：各任务开始 markBusy、结束 markIdle，新增任务无需再改这里。
func (a *App) IsBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeTasks > 0
}

// IsExitConfirmed 用户是否已在前端确认框点击「继续退出」
func (a *App) IsExitConfirmed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitConfirmed
}

// ConfirmExit 用户在前端确认框点击「继续退出」：标记已确认，
// 随后前端调用 runtime.Quit() 时窗口关闭拦截将直接放行
func (a *App) ConfirmExit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.exitConfirmed = true
}

// emitDepsProgress 向界面推送依赖安装进度（百分比 + 阶段说明）
func (a *App) emitDepsProgress(percent int, stage string) {
	runtime.EventsEmit(a.ctx, "deps:progress", map[string]interface{}{"percent": percent, "stage": stage})
}

// CreateVenv 使用物理 Python 新建虚拟环境，返回新建环境的完整信息。
// 说明：虚拟环境版本 = 物理 Python 版本；创建的环境会保留在所选路径，由用户自行管理。
func (a *App) CreateVenv(basePython, venvPath string) (config.PythonEnv, error) {
	return env.CreateVenv(basePython, venvPath, func(line string) {
		runtime.EventsEmit(a.ctx, "deps:log", line)
	}, a.emitDepsProgress)
}

// ScanResources 扫描程序目录下的可打包资源（程序与资源一起打包）
func (a *App) ScanResources(programDir string) []config.ResourceItem {
	items, err := builder.ScanResources(programDir)
	if err != nil {
		return []config.ResourceItem{}
	}
	return items
}

// ---- 打包执行与清理 ----

// Build 启动一次异步打包任务，结果通过 "build:done" 事件通知
func (a *App) Build(cfg config.BuildConfig) error {
	a.mu.Lock()
	if a.cancelBuild != nil {
		close(a.cancelBuild)
	}
	ch := make(chan struct{})
	a.cancelBuild = ch
	a.mu.Unlock()

	go a.doBuild(cfg, ch)
	return nil
}

// CancelBuild 取消当前打包任务
func (a *App) CancelBuild() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelBuild != nil {
		close(a.cancelBuild)
		a.cancelBuild = nil
	}
}

// doBuild 实际执行打包：临时工作区 -> 打包 -> 收集产物 -> 清理
func (a *App) doBuild(cfg config.BuildConfig, cancel chan struct{}) {
	emit := func(evt string, data interface{}) {
		runtime.EventsEmit(a.ctx, evt, data)
	}
	emitLog := func(line string) {
		emit("build:log", line)
	}

	finish := func(success bool, message string, artifacts []string) {
		emit("build:done", map[string]interface{}{
			"success":   success,
			"message":   message,
			"artifacts": artifacts,
		})
	}

	// 无论结果如何，任务结束后清空 cancelBuild + 解除忙碌标记，避免窗口关闭拦截误判"正在执行"
	defer func() {
		a.markIdle()
		a.mu.Lock()
		a.cancelBuild = nil
		a.mu.Unlock()
	}()
	a.markBusy()

	ws, err := builder.NewWorkspace()
	if err != nil {
		finish(false, err.Error(), nil)
		return
	}
	// 无论成功失败，都清理临时工作区，但绝不触碰用户 Python 环境
	defer func() {
		if cerr := ws.Cleanup(); cerr != nil {
			emitLog("[清理] 警告: " + cerr.Error())
		} else {
			emitLog("[清理] 临时文件已全部清理")
		}
	}()

	if err := ws.Run(&cfg, emitLog, cancel); err != nil {
		finish(false, err.Error(), nil)
		return
	}

	// 输出目录
	outDir := cfg.OutputDir
	if outDir == "" {
		outDir = "."
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		finish(false, "输出目录无效: "+err.Error(), nil)
		return
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		finish(false, "创建输出目录失败: "+err.Error(), nil)
		return
	}

	artifacts, err := ws.CollectArtifact(&cfg, absOut)
	if err != nil {
		finish(false, err.Error(), nil)
		return
	}
	// 图标已嵌入产物，但 Windows 资源管理器按名称缓存图标，同名覆盖后仍显示旧图标。
	// 主动通知系统刷新，避免用户误以为图标没打进去
	builder.RefreshIconCache()
	finish(true, "打包成功！", artifacts)
}

// ---- 配置保存 / 导入 / 自动记忆 ----
// （自动记忆配置统一在 internal/paths：%LOCALAPPDATA%\dabao\config.json）
// SaveConfigToFile 弹出保存窗口，把当前打包配置导出为 JSON 文件。
// 导出的配置不包含 Python 环境路径（换电脑后路径会失效，需重新选择环境）。
func (a *App) SaveConfigToFile(cfg config.BuildConfig) (string, error) {
	out, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "保存打包配置",
		DefaultDirectory: paths.ConfigFileDir(),
		DefaultFilename:  "打包配置.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "配置文件", Pattern: "*.json"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || out == "" {
		return "", fmt.Errorf("已取消")
	}
	// 分享/备份用的配置不保存环境路径
	cfg.PythonPath = ""
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("生成配置内容失败: %v", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return "", fmt.Errorf("保存配置失败: %v", err)
	}
	return out, nil
}

// LoadConfigFromFile 弹出打开窗口，读取一个 JSON 打包配置并返回给前端
func (a *App) LoadConfigFromFile() (config.BuildConfig, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入打包配置",
		Filters: []runtime.FileFilter{
			{DisplayName: "配置文件", Pattern: "*.json"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || file == "" {
		return config.BuildConfig{}, fmt.Errorf("已取消")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return config.BuildConfig{}, fmt.Errorf("读取配置失败: %v", err)
	}
	var c config.BuildConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return config.BuildConfig{}, fmt.Errorf("配置文件格式不正确: %v", err)
	}
	normalizeConfig(&c)
	return c, nil
}

// SaveAutoConfig 自动记忆：把当前全部配置（含环境路径）保存到数据目录。
// 下次打开软件自动还原，省去重复设置。
func (a *App) SaveAutoConfig(cfg config.BuildConfig) error {
	if err := os.MkdirAll(paths.ConfigFileDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(paths.ConfigFilePath(), data, 0o644)
}

// DeleteAutoConfig 关闭记忆功能时删除自动记忆文件（config.json）。
// 只删除配置文件本身，不碰数据目录（目录里还有便携版、分析缓存等）。
func (a *App) DeleteAutoConfig() error {
	if err := os.Remove(paths.ConfigFilePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除记忆文件失败: %v", err)
	}
	return nil
}

// LoadAutoConfig 读取自动记忆的配置；没有记忆文件或文件损坏时返回空配置（从默认开始）
func (a *App) LoadAutoConfig() (config.BuildConfig, error) {
	data, err := os.ReadFile(paths.ConfigFilePath())
	if err != nil {
		return config.BuildConfig{}, nil
	}
	var c config.BuildConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return config.BuildConfig{}, nil
	}
	normalizeConfig(&c)
	return c, nil
}

// normalizeConfig 补齐配置里可能缺失的数组字段。
// 旧版本或手动编写的 JSON 里没有 resources/hiddenImports 等字段，
// 反序列化后是 nil，若直接传给前端会变成 null 导致界面渲染崩溃（白屏）。
func normalizeConfig(c *config.BuildConfig) {
	if c.Resources == nil {
		c.Resources = []config.ResourceItem{}
	}
	if c.HiddenImports == nil {
		c.HiddenImports = []string{}
	}
	if c.ExcludeModules == nil {
		c.ExcludeModules = []string{}
	}
	if c.ExtraArgs == nil {
		c.ExtraArgs = []string{}
	}
}

// DetectPythonPath 直接检测一个 python.exe 路径并返回环境信息。
// 用于还原自动记忆的环境：当自动搜索（不含自定义根目录）找不到该环境时，
// 按路径直接检测，保证"记住的 Python"一定能还原并继续打包。
func (a *App) DetectPythonPath(path string) (config.PythonEnv, error) {
	e, err := env.Detect(path)
	if err != nil {
		return config.PythonEnv{}, fmt.Errorf("无法识别的 Python 环境: %v", err)
	}
	e.Name = env.DisplayName(&e)
	return e, nil
}

// PreviewCommand 生成「开始打包」将要执行的 PyInstaller 完整命令，供用户预览与复制。
// 注意：启用代码加密时，实际打包会先加密再打包，命令与预览略有差异（此处为基础命令）。
func (a *App) PreviewCommand(cfg config.BuildConfig) (string, error) {
	if cfg.PythonPath == "" {
		return "", fmt.Errorf("请先选择 Python 环境")
	}
	if cfg.EntryScript == "" {
		return "", fmt.Errorf("请先选择入口脚本")
	}
	ws, err := builder.NewWorkspace()
	if err != nil {
		return "", err
	}
	defer func() { _ = ws.Cleanup() }()
	args, err := builder.BuildArgs(&cfg, ws.Root, cfg.EntryScript, nil, "", nil)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	// 首行：Python 环境（带物理/虚拟标识，便于确认用的是哪个环境）
	envInfo := cfg.PythonPath
	if detected, err := env.Detect(cfg.PythonPath); err == nil {
		envInfo = env.DisplayName(&detected)
	}
	sb.WriteString("# Python 环境：" + envInfo)
	// 图标信息（便于用户核对打包时用的是哪个图标）
	sb.WriteString("\n# 程序图标：")
	if cfg.IconPath != "" {
		sb.WriteString(cfg.IconPath)
		if !strings.EqualFold(filepath.Ext(cfg.IconPath), ".ico") {
			sb.WriteString("（非 .ico 将自动转换为 .ico）")
		}
	} else {
		sb.WriteString("未设置（将使用默认图标）")
	}
	// 完整打包命令（保持原样可复制）
	sb.WriteString("\n\n" + strings.Join(append([]string{cfg.PythonPath}, args...), " "))
	return sb.String(), nil
}
