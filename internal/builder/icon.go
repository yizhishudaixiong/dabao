package builder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/image/draw"
)

// shell32.SHChangeNotify：通知系统资源管理器刷新文件图标缓存。
// PyInstaller 打包的 exe 图标实际已正确嵌入，但 Windows 按「文件完整路径」缓存图标，
// 同名文件被重新打包覆盖后，资源管理器仍显示旧图标（用户误以为图标没打进去）。
// 调用该 API 可强制刷新，PyInstaller 官方文档推荐的可靠方案。
var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	shChangeNotify = shell32.NewProc("SHChangeNotify")
)

// RefreshIconCache 通知 Windows 刷新图标缓存（SHCNE_ASSOCCHANGED | SHCNF_IDLIST）
func RefreshIconCache() {
	// 0x08000000 = SHCNE_ASSOCCHANGED, 0x0000 = SHCNF_IDLIST
	_, _, _ = shChangeNotify.Call(0x08000000, 0x0000, 0, 0)
}

// icoSizes 生成 ICO 时包含的尺寸（Windows 图标标准尺寸）
var icoSizes = []int{256, 128, 64, 48, 32, 16}

// ConvertToIco 将 PNG/JPG 图片转换为多尺寸 .ico 文件（PyInstaller 只接受 .ico）。
// 图像以 PNG 数据存储在 ICO 中（现代 Windows 均支持），透明通道完整保留；
// JPG 无透明通道时自动以不透明背景处理。
func ConvertToIco(srcPath, dstPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("无法解码图片 %s: %v", srcPath, err)
	}

	type entry struct {
		sizeByte uint8 // 0 表示 256
		data     []byte
	}
	var entries []entry
	for _, s := range icoSizes {
		rgba := image.NewRGBA(image.Rect(0, 0, s, s))
		draw.CatmullRom.Scale(rgba, rgba.Bounds(), src, src.Bounds(), draw.Over, nil)

		var buf bytes.Buffer
		if err := png.Encode(&buf, rgba); err != nil {
			return fmt.Errorf("PNG 编码失败: %v", err)
		}
		sizeByte := uint8(s)
		if s == 256 {
			sizeByte = 0
		}
		entries = append(entries, entry{sizeByte: sizeByte, data: buf.Bytes()})
	}

	// 组装 ICO：ICONDIR + ICONDIRENTRY * N + 图像数据
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	dirSize := 6 + 16*len(entries)
	offset := uint32(dirSize)
	var dirs bytes.Buffer
	for _, e := range entries {
		_ = binary.Write(&dirs, binary.LittleEndian, e.sizeByte)    // width (0=256)
		_ = binary.Write(&dirs, binary.LittleEndian, e.sizeByte)    // height
		_ = binary.Write(&dirs, binary.LittleEndian, byte(0))       // colorCount
		_ = binary.Write(&dirs, binary.LittleEndian, byte(0))       // reserved
		_ = binary.Write(&dirs, binary.LittleEndian, uint16(1))     // planes
		_ = binary.Write(&dirs, binary.LittleEndian, uint16(32))    // bitCount
		_ = binary.Write(&dirs, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(&dirs, binary.LittleEndian, offset)
		offset += uint32(len(e.data))
	}
	out.Write(dirs.Bytes())
	for _, e := range entries {
		out.Write(e.data)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dstPath, out.Bytes(), 0o644)
}
