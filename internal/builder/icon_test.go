package builder

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConvertToIco 验证 PNG/JPG → 多尺寸 ICO 转换
func TestConvertToIco(t *testing.T) {
	src := filepath.Join("..", "..", "frontend", "src", "assets", "icon.png")
	dst := "/tmp/convert_test_out.ico"
	if err := ConvertToIco(src, dst); err != nil {
		t.Fatalf("ConvertToIco: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	// ICONDIR(6) + ICONDIRENTRY(16) * 6
	if len(data) < 6+16*6 {
		t.Fatalf("ICO 文件过小: %d 字节", len(data))
	}
	// 目录计数应为 6
	count := int(data[4]) | int(data[5])<<8
	if count != 6 {
		t.Fatalf("预期 6 个尺寸, 实际 %d", count)
	}
	t.Logf("ICO 生成成功: %d 字节, %d 个尺寸", len(data), count)
}
