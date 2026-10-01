// Package paths 统一管理本工具在用户机器上的数据目录（%LOCALAPPDATA%\dabao 下）。
// 多个模块（便携版、分析缓存等）共用同一套路径规则，避免各处重复拼路径、兜底不一致。
package paths

import (
	"os"
	"path/filepath"
)

// LocalAppData 返回用户本地应用数据目录：
// 优先 %LOCALAPPDATA%；异常缺失时回退到「用户目录\AppData\Local」（与 Windows 默认一致）。
func LocalAppData() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return local
	}
	if user := os.Getenv("USERPROFILE"); user != "" {
		return filepath.Join(user, "AppData", "Local")
	}
	return filepath.Join(os.Getenv("HOME"), "AppData", "Local")
}

// DabaoRoot 本工具的数据根目录：%LOCALAPPDATA%\dabao
func DabaoRoot() string {
	return filepath.Join(LocalAppData(), "dabao")
}

// RuntimeRoot 内置便携版 Python 存放目录：%LOCALAPPDATA%\dabao\runtime
func RuntimeRoot() string {
	return filepath.Join(DabaoRoot(), "runtime")
}

// PyiCacheRoot 依赖分析缓存目录：%LOCALAPPDATA%\dabao\pyi_cache
func PyiCacheRoot() string {
	return filepath.Join(DabaoRoot(), "pyi_cache")
}

// TempRoot 本工具在系统临时目录下的统一根目录：%TEMP%\PythonPackTool
// （打包临时工作区 job_*、内嵌 UPX 释放目录都在这里，清理时按此根目录白名单校验）
func TempRoot() string {
	return filepath.Join(os.TempDir(), "PythonPackTool")
}

// UpxDir 内嵌 UPX 释放目录：%TEMP%\PythonPackTool\upx
func UpxDir() string {
	return filepath.Join(TempRoot(), "upx")
}

// ConfigFileDir 自动记忆配置所在目录：%LOCALAPPDATA%\dabao（与数据根目录一致）
func ConfigFileDir() string {
	return DabaoRoot()
}

// ConfigFilePath 自动记忆配置文件的完整路径：%LOCALAPPDATA%\dabao\config.json
func ConfigFilePath() string {
	return filepath.Join(DabaoRoot(), "config.json")
}
