// 资源打包目标路径计算与 UPX 内嵌支持（builder 包）

package builder

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"pypacker/internal/paths"
)

// 与前端"打包后路径"显示保持一致的资源目标路径规则：
// - 程序目录内：保留相对入口脚本目录的层级（文件取相对路径；文件夹取相对路径本身）
// - 程序目录外（额外添加）：文件放资源根（文件名）；文件夹保留文件夹本身及内部结构（文件夹名）
func resourceTarget(abs, projDir string, isDir bool) string {
	if projDir != "" && isWithin(abs, projDir) {
		rel, err := filepath.Rel(projDir, abs)
		if err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) {
			return rel
		}
	}
	return filepath.Base(abs)
}

// UPX 内嵌：upx/upx.exe 在编译时被 go:embed 打进工具本体，
// 用户勾选"UPX 压缩"时无需联网下载，运行时释放到临时目录供 PyInstaller 使用。
// 注：UPX 为 GPL v2 协议开源软件，分发时已在"关于"页注明来源与许可。
//go:embed all:upx/upx.exe
var upxFS embed.FS

var (
	upxOnce sync.Once
	upxDir  string
	upxErr  error
)

// EnsureUPX 确保内嵌的 UPX 已释放到临时目录，返回其所在目录（进程内只释放一次）
func EnsureUPX() (string, error) {
	upxOnce.Do(func() {
		dir := paths.UpxDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			upxErr = fmt.Errorf("创建 UPX 目录失败: %v", err)
			return
		}
		exe := filepath.Join(dir, "upx.exe")
		// 已存在且内容一致则直接复用（避免每次重复写盘）
		if data, err := os.ReadFile(exe); err == nil && len(data) > 0 {
			if in, ierr := upxFS.ReadFile("upx/upx.exe"); ierr == nil && len(data) == len(in) {
				upxDir = dir
				return
			}
		}
		bin, err := upxFS.ReadFile("upx/upx.exe")
		if err != nil {
			upxErr = fmt.Errorf("读取内嵌 UPX 失败: %v", err)
			return
		}
		if err := os.WriteFile(exe, bin, 0o755); err != nil {
			upxErr = fmt.Errorf("释放 UPX 失败: %v", err)
			return
		}
		upxDir = dir
	})
	return upxDir, upxErr
}
