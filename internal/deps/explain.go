package deps

import (
	"strings"

	"pypacker/internal/errhint"
)

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
