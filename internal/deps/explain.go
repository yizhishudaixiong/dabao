package deps

import (
	"regexp"
	"strings"

	"pypacker/internal/errhint"
)

// scriptNotFoundRegex 匹配 PyInstaller 的入口脚本不存在报错（捕获组 1 为具体路径）：
// ERROR: script 'D:\proj\main.py' not found
var scriptNotFoundRegex = regexp.MustCompile(`(?i)script\s+'([^']+)'\s+not found`)

// pipRules 依赖安装失败规则表：按优先级从上到下匹配，命中即返回对应中文原因+建议。
// 关键词均会先转小写再匹配 —— 越具体、越常见的错误越靠前。
var pipRules = []errhint.Rule{
	// 1. pip 组件缺失/损坏
	{Keywords: []string{"no module named pip"},
		Reason: "所选 Python 环境的 pip 组件缺失或损坏",
		Advice: "请重新选择 Python 环境；或在命令行执行 python -m ensurepip --upgrade 修复该环境后重试。"},

	// 2. 网络超时 / 连接失败（国内访问官方源最常见的错误）
	{Keywords: []string{"read timed out", "timed out", "connectionerror", "newconnectionerror", "network is unreachable", "connection refused"},
		Reason: "网络连接失败或下载超时",
		Advice: "网络不稳定时建议切换「清华源」或「阿里源」后重试；也可稍后再试。"},

	// 3. 连接被中断（网络波动 / 代理干扰）
	{Keywords: []string{"connectionreseterror", "proxyerror", "connection aborted", "eof occurred", "connection reset"},
		Reason: "下载连接被中断（网络波动或代理干扰）",
		Advice: "请切换「阿里源」或「清华源」重试；若电脑开了代理，请检查代理是否正常。"},

	// 4. 镜像源不可用 / 包在源上不存在
	{Keywords: []string{"could not find a version", "no matching distribution", "404 not found", "not found for url"},
		Reason: "所选下载源中找不到该依赖（或版本不存在）",
		Advice: "尝试切换其他下载源（清华/阿里/官方）；若依赖名称或版本号拼写有误，请检查后重试。"},

	// 5. 下载文件校验值不符（文件被篡改或缓存损坏）
	{Keywords: []string{"do not match the hashes", "hash mismatch", "hashes from the requirements"},
		Reason: "下载的安装包校验值不符（文件被篡改或本地缓存损坏）",
		Advice: "请切换其他下载源重试；同时可在依赖检查页先「清理缓存」（pip cache purge）再安装。"},

	// 6. 下载的文件损坏 / 解压失败
	{Keywords: []string{"badzipfile", "file is not a zip file", "is not a valid wheel", "error unpacking", "cannot unpack file", "cannot determine archive format"},
		Reason: "下载的安装包文件损坏或不完整（多为网络中途断流导致）",
		Advice: "请切换「阿里源」或「清华源」后重试；若仍失败，可在依赖检查页先点「清理缓存」再安装。"},

	// 7. 需要编译源码失败（Windows 上最常见：缺 C 编译环境或该包无预编译包）
	{Keywords: []string{"subprocess-exited-with-error", "building wheel", "building '", "cl.exe", "fatal error: python.h", "vcvarsall", "error: command '"},
		Reason: "该依赖需要编译源码，但当前环境缺少编译工具或编译失败",
		Advice: "优先安装官方预编译版本：包名带 -binary 的请安装 xxx-binary（如 psycopg2-binary）；确实需要编译的包，请先安装 Visual Studio Build Tools（勾选「使用 C++ 的桌面开发」）后重试。"},

	// 8. 依赖版本冲突 / Python 版本要求不满足
	{Keywords: []string{"conflicting dependencies", "resolutionimpossible", "requires-python", "pip's dependency resolver"},
		Reason: "依赖之间存在版本冲突，或要求不兼容的 Python 版本",
		Advice: "可去掉指定版本号让 pip 自动选择兼容版本；或改用其他 Python 版本的环境。"},

	// 9. 旧包与新版 Python 不兼容（如 Python 3.12 装老包缺 distutils）
	{Keywords: []string{"legacy-install-failure", "distutils", "no module named 'setuptools'", "no module named 'pkg_resources'"},
		Reason: "依赖包过旧，与当前 Python 版本不兼容（新版 Python 已移除 distutils 等旧组件）",
		Advice: "请在依赖检查页先安装/升级 setuptools 和 wheel 后重试；或改用 Python 3.10/3.11 的环境。"},

	// 10. 权限不足
	{Keywords: []string{"permissionerror", "access is denied", "winerror 5", "winerror 32"},
		Reason: "安装目录没有写入权限（或被占用）",
		Advice: "请关闭占用该 Python 环境的程序（如已打开的命令行、IDE）；若环境装在系统目录，建议使用虚拟环境安装。"},

	// 11. 磁盘空间不足
	{Keywords: []string{"no space left", "insufficient disk", "disk full"},
		Reason: "磁盘空间不足",
		Advice: "请清理磁盘空间后重试。"},

	// 12. 内存不足（编译/解析大依赖时偶发）
	{Keywords: []string{"memoryerror", "out of memory"},
		Reason: "安装过程中内存不足",
		Advice: "请关闭其他占用内存的程序后重试；也可在依赖检查页分批安装依赖。"},

	// 13. SSL 证书校验失败
	{Keywords: []string{"certificate verify", "ssl error", "ssl module", "ssl certificate"},
		Reason: "SSL 证书校验失败",
		Advice: "通常是系统时间不对或网络代理干扰，请校准系统时间后重试。"},

	// 14. 依赖名称/版本格式不合法
	{Keywords: []string{"invalid requirement"},
		Reason: "依赖名称或版本格式不合法",
		Advice: "请检查依赖列表中的名称与版本号格式（如 包名==版本号）是否正确。"},

	// 15. 平台架构不匹配（无适配当前 Python 位数的预编译包）
	{Keywords: []string{"not a supported wheel", "does not support this platform"},
		Reason: "该依赖没有适配当前 Python 位数/系统的预编译安装包",
		Advice: "请改用 64 位 Python 环境重试；部分库只提供源码包，可尝试安装对应编译版（如 xxx-binary）。"},

	// 16. 环境存在残留的损坏包目录（pip 警告）
	{Keywords: []string{"ignoring invalid distribution"},
		Reason: "Python 环境中存在残留的损坏包目录（pip 警告）",
		Advice: "该警告通常可忽略；若影响安装，请到该环境 site-packages 目录下删除对应残留文件夹。"},
}

// ExplainPipError 分析 pip 完整输出，识别常见失败原因，返回中文原因 + 建议。
// 识别不了时给出通用兜底。
func ExplainPipError(out string) errhint.Hint {
	return errhint.Match(strings.ToLower(out), pipRules, errhint.Hint{
		Reason: "依赖安装失败（未能自动识别具体原因）",
		Advice: "请查看下方「最近输出」中最后几行排查；也可截图发给作者协助定位。",
	})
}

// BuildPipDiagnosis 拼装完整中文诊断文本（追加到安装日志最底部，并作为错误消息返回）
func BuildPipDiagnosis(out string) string {
	h := ExplainPipError(out)
	return errhint.BuildDiagnosis("依赖安装诊断", h, out, 10)
}

// analyzeRules 依赖分析（PyInstaller 官方分析引擎）失败规则表：
// 按优先级从上到下匹配，命中即返回对应中文原因+建议。
// 关键词均会先转小写再匹配 —— 越具体、越常见的错误越靠前。
var analyzeRules = []errhint.Rule{
	// 1. 入口脚本不存在 / 路径错误（最常见；PyInstaller 与 Python 两级报错都覆盖）
	{Regex: scriptNotFoundRegex,
		Reason: "找不到入口脚本文件：{1}（路径不存在，或文件已被移动/删除）",
		Advice: "请回到「程序信息」页重新选择入口脚本；确认脚本文件没有被删除或改名，路径中没有多余字符。"},
	{Keywords: []string{"filenotfounderror", "no such file or directory", "[errno 2]"},
		Reason: "找不到入口脚本文件（路径不存在，或文件已被移动/删除）",
		Advice: "请回到「程序信息」页重新选择入口脚本；确认脚本文件没有被删除或改名，路径中没有多余字符。"},

	// 2. 入口脚本是文件夹 / 无读取权限
	{Keywords: []string{"is a directory", "permission denied"},
		Reason: "入口脚本是文件夹或没有读取权限",
		Advice: "请回到「程序信息」页选择正确的 .py 脚本文件（不要选文件夹），并确认文件未被其他程序占用。"},

	// 3. 所选环境未安装 PyInstaller（分析引擎本身缺失）
	{Keywords: []string{"no module named pyinstaller"},
		Reason: "所选 Python 环境未安装 PyInstaller（分析引擎依赖它）",
		Advice: "分析前本工具会自动补装 PyInstaller；这里仍报错说明自动安装未成功，请到「依赖检查」页手动安装 PyInstaller，或切换下载源（阿里源/清华源）后重试。"},

	// 4. Python 环境不完整（缺核心库）
	{Keywords: []string{"python library not found", "libpython"},
		Reason: "所选 Python 环境不完整（缺少 Python 核心库文件）",
		Advice: "该环境无法用于依赖分析，请在「选择环境」页重新选择完整安装的 Python，或新建虚拟环境后重试。"},

	// 5. Python 环境损坏（缺失基础模块）
	{Keywords: []string{"no module named 'encodings'", "no module named encodings"},
		Reason: "所选 Python 环境已损坏（缺失基础模块 encodings）",
		Advice: "该环境无法正常使用，请在「选择环境」页重新选择，或新建虚拟环境后重试。"},

	// 6. PyInstaller 与 Python 版本不兼容
	{Keywords: []string{"your system is not supported", "pyinstaller requires at least", "unsupported python", "not a valid pyinstaller"},
		Reason: "PyInstaller 与当前 Python 版本不兼容",
		Advice: "请在「依赖检查」页把 PyInstaller 升级/重装到最新版后重试；或改用官方推荐的 Python 3.10~3.12 环境。"},

	// 7. 源码语法错误
	{Keywords: []string{"syntaxerror", "indentationerror"},
		Reason: "入口脚本或项目代码存在语法错误",
		Advice: "请先在本地用 Python 运行一遍你的程序，修复报错后再点击分析。"},

	// 8. 源码编码不是 UTF-8
	{Keywords: []string{"unicodedecodeerror", "codec can't", "gbk codec", "utf-8 codec"},
		Reason: "源码文件编码不是 UTF-8，分析引擎无法解析",
		Advice: "请用编辑器把 .py 源码另存为 UTF-8 编码后再试。"},

	// 9. 分析引擎处理某个依赖库时出错（库缺失 / 损坏 / 钩子失败）
	{Keywords: []string{"hook failed", "failed to execute script", "importerror", "attributeerror", "typeerror"},
		Reason: "分析引擎处理某个依赖库时出错（该库缺失或已损坏）",
		Advice: "请查看下方「最近输出」确认是哪个库报错，到「依赖检查」页安装或强制重装该库后重试；若勾选了「精简打包」，请先关闭再试。"},

	// 10. 递归深度超限（大项目常见）
	{Keywords: []string{"recursionerror", "maximum recursion depth exceeded"},
		Reason: "分析时 Python 递归深度超限（代码或依赖嵌套过深）",
		Advice: "可在入口脚本最上方加入以下两行后重试：\nimport sys; sys.setrecursionlimit(10000)"},

	// 11. 内存不足
	{Keywords: []string{"memoryerror", "out of memory"},
		Reason: "分析时内存不足",
		Advice: "请关闭其他占用内存的程序后重试；大项目建议改用「多文件」模式打包。"},

	// 12. 磁盘空间不足
	{Keywords: []string{"no space left", "insufficient disk", "disk full"},
		Reason: "磁盘空间不足（分析引擎需要临时空间）",
		Advice: "请清理磁盘空间后重试。"},

	// 13. 权限不足 / 被杀毒软件拦截
	{Keywords: []string{"permissionerror", "access is denied", "winerror 5", "winerror 32"},
		Reason: "分析过程没有写入权限，或文件被占用/被杀毒软件拦截",
		Advice: "请关闭正在运行的目标程序；将本工具、项目目录加入杀毒软件白名单后重试。"},
}

// ExplainAnalyzeError 分析依赖分析引擎的完整输出，识别失败原因，
// 返回中文原因 + 针对性建议（识别不了时给出通用兜底）。
func ExplainAnalyzeError(out string) errhint.Hint {
	return errhint.Match(strings.ToLower(out), analyzeRules, errhint.Hint{
		Reason: "依赖分析失败（未能自动识别具体原因）",
		Advice: "请查看下方「最近输出」中最后几行排查；也可截图发给作者协助定位。",
	})
}

// BuildAnalyzeDiagnosis 拼装完整中文诊断文本（作为分析失败的最终错误消息返回，
// 含中文原因 + 建议 + 分析引擎最近输出原文）。
func BuildAnalyzeDiagnosis(out string) string {
	h := ExplainAnalyzeError(out)
	return errhint.BuildDiagnosis("依赖分析诊断", h, out, 10)
}
