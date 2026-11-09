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

// PyInstaller 的 --add-data DEST 是「目标目录」，文件名由它自己追加。
// 单文件如果把带文件名的路径当 DEST，产物里会变成 "<文件名>/<文件名>"，
// 程序按 sys._MEIPASS 拼原路径时打开的是个目录 → FileNotFoundError/PermissionError。
func TestResourceTarget(t *testing.T) {
	proj := filepath.Join("D:", "proj")
	cases := []struct {
		name   string
		abs    string
		isDir  bool
		expect string
	}{
		{"项目根下的单文件", filepath.Join(proj, "config.json"), false, "."},
		{"子目录里的单文件", filepath.Join(proj, "assets", "logo.png"), false, "assets"},
		{"深层子目录里的单文件", filepath.Join(proj, "a", "b", "x.txt"), false, filepath.Join("a", "b")},
		{"项目内的文件夹", filepath.Join(proj, "assets"), true, "assets"},
		{"项目外的单文件", filepath.Join("D:", "other", "x.png"), false, "."},
		{"项目外的文件夹", filepath.Join("D:", "other", "data"), true, "data"},
	}
	for _, c := range cases {
		if got := resourceTarget(c.abs, proj, c.isDir); got != c.expect {
			t.Errorf("%s: resourceTarget=%q, 期望 %q", c.name, got, c.expect)
		}
	}
}
