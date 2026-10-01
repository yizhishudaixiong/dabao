package builder

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureUPX 验证内嵌的 UPX 能正确释放到临时目录且非空（回归测试）
func TestEnsureUPX(t *testing.T) {
	dir, err := EnsureUPX()
	if err != nil {
		t.Fatalf("EnsureUPX failed: %v", err)
	}
	exe := filepath.Join(dir, "upx.exe")
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("upx.exe not found: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("upx.exe is empty")
	}
}
