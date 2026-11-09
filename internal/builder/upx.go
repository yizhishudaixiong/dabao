// 资源打包目标路径计算与 UPX 内嵌支持（builder 包）

package builder

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"pypacker/internal/fsx"
	"pypacker/internal/paths"
)

// resourceTarget 计算 --add-data 的 DEST。
//
// 关键：PyInstaller 的 DEST 语义是「目标**目录**」，文件名由它自己追加
// （见 PyInstaller/building/utils.py: format_binaries_and_datas，
//  文件分支是 os.path.join(trg_root_dir, os.path.basename(src))）。
// 因此：
//   - 文件夹 → DEST 就是它自己（内部结构原样保留）
//   - 单文件 → DEST 必须是它所在的目录，否则会被放进 "<文件名>/<文件名>"，
//     程序按原路径 open() 直接报 FileNotFoundError（曾经踩过）
//
// 打包后的最终位置（前端展示用）：项目内文件 = 相对入口脚本目录的路径；
// 项目外单文件 = 包根目录；文件夹 = 文件夹本身。
func resourceTarget(abs, projDir string, isDir bool) string {
	if projDir != "" && fsx.IsWithin(abs, projDir) {
		rel, err := filepath.Rel(projDir, abs)
		if err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) {
			if isDir {
				return rel
			}
			return filepath.Dir(rel) // 项目根下的单文件 -> "."
		}
	}
	if isDir {
		return filepath.Base(abs)
	}
	return "." // 项目外的单文件放包根
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
		// 已存在且内容一致则直接复用（避免每次重复写盘）。
		// 必须逐字节比对：只比长度的话，长度相同但内容损坏的文件会被当成"已释放"复用，
		// 后续每次 UPX 压缩都会失败，且很难排查。
		if data, err := os.ReadFile(exe); err == nil && len(data) > 0 {
			if in, ierr := upxFS.ReadFile("upx/upx.exe"); ierr == nil && bytes.Equal(data, in) {
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
