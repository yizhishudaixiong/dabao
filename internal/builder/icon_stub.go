//go:build !windows

// 非 Windows 平台的空实现：保证 RefreshIconCache 在任何平台都有定义，
// 满足 Linux 交叉编译与云端 CI 宿主检查；非 Windows 平台无图标缓存可刷新，故为空操作。
package builder

// RefreshIconCache 非 Windows 平台为空操作
func RefreshIconCache() {}
