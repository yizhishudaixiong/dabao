package builder

import (
	"regexp"
	"strings"

	"pypacker/internal/errhint"
)

// moduleNotFoundRegex 匹配缺模块报错，提取模块名（捕获组 1 替换 {1} 占位）
var moduleNotFoundRegex = regexp.MustCompile(`(?i)ModuleNotFoundError: No module named ['"]([^'"]+)['"]`)

// buildRules 打包失败规则表：按优先级从上到下匹配，命中即返回对应中文原因+建议。
// 关键词均会先转小写再匹配 —— 越具体、越常见的错误越靠前。
var buildRules = []errhint.Rule{
	// 1. 所选环境未安装 PyInstaller（报错格式：xxx\python.exe: No module named PyInstaller）
	{Keywords: []string{"no module named pyinstaller"},
		Reason: "所选 Python 环境未安装 PyInstaller（打包核心组件）",
		Advice: "回到「依赖检查」页，点击「分析依赖」后勾选安装 PyInstaller；或重新选择已安装 PyInstaller 的 Python 环境。"},

	// 2. Python 环境损坏（缺失 encodings 基础模块）
	{Keywords: []string{"no module named 'encodings'", "no module named encodings"},
		Reason: "所选 Python 环境已损坏（缺失基础模块 encodings）",
		Advice: "该环境无法正常打包，请在「选择环境」页重新选择，或新建虚拟环境后重试。"},

	// 3. 单文件超过 4GB（PyInstaller 归档上限）
	{Keywords: []string{"struct.error", "4294967295"},
		Reason: "单文件模式产物超过了 PyInstaller 的 4GB 归档上限",
		Advice: "你的项目依赖体积较大（如 PyTorch / TensorRT / PySide6 等），请在「打包模式」页改为「多文件」模式打包。"},

	// 4. 递归超限（大项目/复杂依赖常见）
	{Keywords: []string{"recursionerror", "maximum recursion depth exceeded"},
		Reason: "打包时 Python 递归深度超限（项目代码或依赖库嵌套过深）",
		Advice: "这是 PyInstaller 分析大型项目时的常见问题。可在入口脚本最上方加入以下两行后重试：\nimport sys; sys.setrecursionlimit(10000)\n若仍失败，请改为「多文件」模式打包。"},

	// 5. 缺少第三方依赖（正则提取模块名）
	{Regex: moduleNotFoundRegex,
		Reason: "程序缺少第三方依赖：{1}",
		Advice: "回到「依赖检查」页安装 {1}；若已勾选「精简打包」，请先关闭再试；若是程序运行中才动态导入的模块，请在「程序信息」页的隐藏导入中手动添加。"},

	// 6. 多进程程序缺少启动引导
	{Keywords: []string{"start a new process before the current process", "freeze_support"},
		Reason: "程序使用了多进程（multiprocessing），打包后缺少启动引导",
		Advice: "请在入口脚本的 if __name__ == '__main__': 内第一行加上 multiprocessing.freeze_support() 后重新打包。"},

	// 7. 同时安装了两套 Qt 界面库（PyQt + PySide 冲突）
	{Keywords: []string{"multiple qt bindings", "hook for 'pyqt5', while hook for 'pyside6'"},
		Reason: "环境中同时安装了 PyQt 和 PySide 两套 Qt 界面库，PyInstaller 无法同时打包",
		Advice: "请在「依赖检查」页卸载其中一套（PyQt5 或 PySide6），只保留一个界面库后再打包。"},

	// 8. Qt 平台插件未打包
	{Keywords: []string{"could not find the qt platform plugin", "cannot find existing pyqt5 plugin", "qt platform plugin"},
		Reason: "Qt 界面库的平台插件未被打包",
		Advice: "请关闭「精简打包」后重试；多文件模式下仍报错时，可将 Python 环境里 site-packages\\PyQt5\\Qt\\plugins 整个文件夹加入「额外资源」。"},

	// 9. 源码语法错误
	{Keywords: []string{"syntaxerror", "indentationerror"},
		Reason: "程序代码存在语法错误（PyInstaller 解析源码失败）",
		Advice: "请先在本地用 Python 运行一遍你的程序，修复报错后再打包。"},

	// 10. 导入阶段执行错误（引用不存在的名字等）
	{Keywords: []string{"nameerror"},
		Reason: "程序在导入阶段执行出错（引用了不存在的名字）",
		Advice: "PyInstaller 会执行源码顶层的 import 代码，请检查顶层是否有会立即报错的语句；先在本地运行修复后再打包。"},

	// 11. 缺少 VC 运行库（lib not found: api-ms-win-cr 等）
	{Keywords: []string{"lib not found", "api-ms-win-cr", "vcruntime140", "msvcp140", "ucrtbase"},
		Reason: "系统缺少 Visual C++ 运行库（UCRT / VC Runtime）",
		Advice: "请安装「微软常用运行库合集」（VC++ 2015-2022 x64），或到微软官网下载 Visual C++ Redistributable 安装后重试。"},

	// 12. 权限不足 / 杀毒软件拦截
	{Keywords: []string{"permissionerror", "access is denied", "winerror 5", "winerror 32"},
		Reason: "打包过程没有写入权限，或文件被占用 / 被杀毒软件拦截",
		Advice: "请关闭正在运行的目标程序；将本工具、输出目录加入杀毒软件白名单后重试；若输出目录在 C 盘系统目录，请更换到普通目录。"},

	// 13. 内存不足
	{Keywords: []string{"memoryerror", "out of memory"},
		Reason: "打包时内存不足",
		Advice: "请关闭其他占用内存的程序后重试；大项目建议改用「多文件」模式，占用内存更小。"},

	// 14. 磁盘空间不足
	{Keywords: []string{"no space left", "insufficient disk", "disk full"},
		Reason: "磁盘空间不足",
		Advice: "请清理磁盘空间后重试（打包临时文件位于系统 Temp 目录，可一并清理）。"},

	// 15. 源码编码不是 UTF-8
	{Keywords: []string{"unicodedecodeerror", "codec can't", "gbk codec", "utf-8 codec"},
		Reason: "源码文件编码不是 UTF-8，PyInstaller 解析失败",
		Advice: "请用编辑器把 .py 源码另存为 UTF-8 编码后再打包。"},

	// 16. 代码加密冲突（PyArmor 与 PyInstaller 版本不匹配）
	{Keywords: []string{"unauthorized use of script", "pyarmor runtime", "runtime error: unauthorized"},
		Reason: "代码加密（PyArmor）与 PyInstaller 版本不匹配，加密后的脚本无法被打包",
		Advice: "请在「依赖检查」页把 pyarmor 和 pyinstaller 都升级到最新版后重试；若仍失败，可暂时关闭代码加密完成打包。"},

	// 17. 依赖库文件收集不完整（部分库只打包了一部分）
	{Keywords: []string{"cannot import name", "could not get source code"},
		Reason: "某个依赖库的文件没有被完整收集（常见于大型库 / 动态导入库）",
		Advice: "请在「程序信息」页的隐藏导入中手动添加报错涉及的模块；若勾选了「精简打包」，请先关闭再试。"},

	// 18. PyInstaller 找不到 Python 核心库
	{Keywords: []string{"python library not found"},
		Reason: "PyInstaller 找不到 Python 核心库（该 Python 环境不完整）",
		Advice: "请在「选择环境」页重新选择完整安装的 Python，或新建虚拟环境后重试。"},

	// 19. 缺少已安装库的元数据（DistributionNotFound）
	{Keywords: []string{"distributionnotfound", "pkg_resources"},
		Reason: "程序缺少某个库的版本信息文件（元数据）",
		Advice: "请在「依赖检查」页强制重装报错提到的库后重试（如 pip install 库名 --force-reinstall）。"},

	// 20. PyInstaller 与 Python 版本不兼容
	{Keywords: []string{"not a valid pyinstaller", "unsupported python", "python version", "your system is not supported", "pyinstaller requires"},
		Reason: "所选 Python 环境中的 PyInstaller 与 Python 版本不兼容",
		Advice: "请在「依赖检查」页把 PyInstaller 升级/重装到最新版后重试。"},

	// 21. OpenCV 资源未收集（打包后常见）
	{Keywords: []string{"opencv loader: missing configuration"},
		Reason: "OpenCV 的配置文件未被打包（cv2 需要附带 config 等资源文件）",
		Advice: "请关闭「精简打包」后重试；若仍失败，可在「打包模式」页的额外资源中手动添加 OpenCV 安装目录下的 cv2 文件夹。"},

	// 22. UPX 压缩失败
	{Keywords: []string{"upx is not available", "upx failed", "upx not available", "upx unpack failed"},
		Reason: "UPX 压缩步骤失败（可能被杀毒软件拦截，或与目标文件不兼容）",
		Advice: "请将本工具加入杀毒软件白名单后重试；也可暂时关闭「UPX 压缩」开关完成打包。"},

	// 23. PyInstaller 引导程序缺失（常被杀软误删）
	{Keywords: []string{"pre-compiled bootloader", "bootloader for your platform", "runw.exe"},
		Reason: "PyInstaller 的引导程序缺失（常被杀毒软件/防火墙误删）",
		Advice: "请在「依赖检查」页重新安装 PyInstaller，并将 Python 环境加入杀毒软件白名单后重试。"},

	// 24. 位数（32/64 位）不匹配
	{Keywords: []string{"not a valid win32 application", "bad cpu type", "wrong architecture"},
		Reason: "打包产物与目标系统的位数（32/64 位）不匹配",
		Advice: "请使用 64 位 Python 环境打包 64 位系统使用的程序（Windows 上建议统一用 64 位）。"},

	// 25. 依赖文件被重复收集（PyInstaller 警告）
	{Keywords: []string{"file already exists but should not"},
		Reason: "有依赖文件被重复收集（PyInstaller 警告）",
		Advice: "该警告通常可忽略；若同时伴随其他报错，可在「依赖检查」页关闭「精简打包」后重试。"},
}

// ExplainBuildError 分析 PyInstaller 完整输出，识别错误类型，
// 返回中文原因 + 针对性建议（识别不了时给出通用兜底）。
func ExplainBuildError(out string) errhint.Hint {
	return errhint.Match(strings.ToLower(out), buildRules, errhint.Hint{
		Reason: "PyInstaller 打包失败（未能自动识别具体原因）",
		Advice: "请查看下方「最近输出」中最后几行排查；也可截图发给作者协助定位。",
	})
}

// BuildDiagnosisText 拼装完整诊断文本（追加到构建日志最底部，同时作为最终错误消息）
func BuildDiagnosisText(hint errhint.Hint, out string) string {
	return errhint.BuildDiagnosis("打包失败诊断", hint, out, 10)
}
