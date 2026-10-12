//go:build windows

// Windows 专属：刷新资源管理器图标缓存。
// 拆出此文件是为了让 Linux 交叉编译（云端 CI）能通过：syscall.NewLazyDLL
// 只在 Windows 平台存在，宿主 Linux 检查代码时必须排除本文件。
package builder

import "syscall"

// shell32.SHChangeNotify：通知系统资源管理器刷新文件图标缓存。
// PyInstaller 打包的 exe 图标实际已正确嵌入，但 Windows 按「文件完整路径」缓存图标，
// 同名文件被重新打包覆盖后，资源管理器仍显示旧图标（用户误以为图标没打进去）。
// 调用该 API 可强制刷新，PyInstaller 官方文档推荐的可靠方案。
var (
	shell32        = syscall.NewLazyDLL("shell32.dll")
	shChangeNotify = shell32.NewProc("SHChangeNotify")
)

// RefreshIconCache 通知 Windows 刷新图标缓存（SHCNE_ASSOCCHANGED | SHCNF_IDLIST）
func RefreshIconCache() {
	// 0x08000000 = SHCNE_ASSOCCHANGED, 0x0000 = SHCNF_IDLIST
	_, _, _ = shChangeNotify.Call(0x08000000, 0x0000, 0, 0)
}
