package config

// PythonEnv 描述一个可用的 Python 环境（系统 Python 或虚拟环境）
type PythonEnv struct {
	Name           string `json:"name"`
	Path           string `json:"path"` // python.exe 完整路径
	Version        string `json:"version"`
	IsVirtual      bool   `json:"isVirtual"`
	HasPip         bool   `json:"hasPip"`
	HasPyInstaller bool   `json:"hasPyInstaller"`
	HasPyarmor     bool   `json:"hasPyarmor"`     // 是否已安装 PyArmor（代码加密用）
	IsPortable     bool   `json:"isPortable"`     // 是否为工具内置的便携版 Python（存放在工具数据目录，非系统安装）
	Root           string `json:"root"`           // 虚拟环境根目录（含 pyvenv.cfg 的目录）；系统环境为空
}

// ResourceItem 描述一个待打包的资源目录/文件
type ResourceItem struct {
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
	IsDir   bool   `json:"isDir"` // 是否为文件夹（用于前端图标区分）
}

// PyarmorInfo PyArmor 检测结果（单对象返回，避免 Wails 多返回值绑定问题）
type PyarmorInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

// BuildConfig 一次打包任务的全部配置
type BuildConfig struct {
	// 程序信息
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	IconPath    string `json:"iconPath"`    // .ico 图标路径
	EntryScript string `json:"entryScript"` // 入口 .py 文件

	// 打包模式
	OneFile     bool           `json:"oneFile"`     // true=单文件 / false=多文件
	NoConsole   bool           `json:"noConsole"`   // true=无控制台窗口
	LiteMode    bool           `json:"liteMode"`    // true=精简打包
	Resources   []ResourceItem `json:"resources"`   // 需要一起打包的资源
	OutputDir   string         `json:"outputDir"`   // 最终 EXE 输出目录
	ContentsDir string         `json:"contentsDir"` // 多文件模式下依赖文件夹名称（留空用默认 _internal）
	HiddenImports []string     `json:"hiddenImports"` // 用户手动补充的隐藏导入模块（动态导入/插件），打包时传给 --hidden-import
	UseUPX        bool         `json:"useUPX"`        // true=使用内置 UPX 压缩产物（体积更小，但可能提高杀软误报率）
	ExcludeModules []string    `json:"excludeModules"` // 用户手动排除的模块（--exclude-module），用于缩小体积

	// 代码加密（PyArmor）："" / "none" 不加密；"basic" 基础加密；"deep" 深度加密
	EncryptMode string `json:"encryptMode"`

	// 环境
	PythonPath string `json:"pythonPath"` // 用户选择的 python.exe

	// 附加 PyInstaller 参数
	ExtraArgs []string `json:"extraArgs"`

	// 打包缓存加速：true=使用 PyInstaller 编译缓存（默认，不传 --clean，打包更快，缓存保留）；
	// false=全新构建（打包前传 --clean 清缓存，打包完成后删除缓存，保证不受旧缓存影响）
	CacheAccel bool `json:"cacheAccel"`

	// 管理员权限运行：true=打包出的程序启动时自动请求管理员权限（--uac-admin，弹 UAC 提权框）
	UacAdmin bool `json:"uacAdmin"`
}

// DepInfo 一个依赖包的状态
type DepInfo struct {
	ImportName       string `json:"importName"`       // import 语句中的名字
	PkgName          string `json:"pkgName"`          // pip 包名
	Version          string `json:"version"`          // requirements.txt 中的版本约束（如 "==1.2.3"），无约束为空
	InstalledVersion string `json:"installedVersion"` // 目标环境中实际已安装的版本号（未安装为空）
	Installed        bool   `json:"installed"`        // 目标环境是否已安装
	Required         bool   `json:"required"`         // 是否必需依赖（如 PyInstaller；选了加密时 PyArmor），缺失时不可取消安装
	Source           string `json:"source"`           // 引用来源（相对路径:行号，多个用逗号分隔），如 "main.py:5, ui.py:12"
}

// DepsResult 依赖分析结果
type DepsResult struct {
	Missing  []DepInfo `json:"missing"`  // 缺失的依赖（需安装）
	Resolved []DepInfo `json:"resolved"` // 已满足的依赖（代码直接 import 且已安装）
	// HookExtra PyInstaller 钩子补充的间接依赖（已装，随主包自动安装，无需单独处理）
	HookExtra []DepInfo `json:"hookExtra"`
	// AnalysisModules PyInstaller 官方分析引擎收集到的第三方模块全名列表
	// （含钩子补充的动态导入模块；供加密打包时全量喂给收集阶段）
	AnalysisModules []string `json:"analysisModules"`
	// AnalysisSeconds PyInstaller 官方分析实际耗时（秒），供界面显示"分析用了多久"
	AnalysisSeconds float64 `json:"analysisSeconds"`
	// IsCached 本次分析是否命中了 PyInstaller 缓存（未重新分析）
	IsCached bool `json:"isCached"`
}

// ResourceDep 代码引用资源与打包清单的对比结果（资源依赖分析）
type ResourceDep struct {
	Ref        string `json:"ref"`        // 代码中的引用写法（如 logo.png、assets/logo.png）
	DiskPath   string `json:"diskPath"`   // 磁盘上解析出的完整路径（动态路径/系统库为空）
	Exists     bool   `json:"exists"`     // 磁盘上是否存在该文件
	Included   bool   `json:"included"`   // 是否已被勾选包含（enabled 资源）
	Kind       string `json:"kind"`       // file=静态引用 / dynamic=动态路径 / sysdll=系统库
	SourceFile string `json:"sourceFile"` // 引用该资源的源文件（相对入口目录）
	Line       int    `json:"line"`       // 引用所在行号
	Code       string `json:"code"`       // 依据代码（引用所在行的源码文本，便于用户定位）
}
