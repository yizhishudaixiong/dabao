package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pypacker/internal/config"
)

func TestMissingModulesRealBuild(t *testing.T) {
	if os.Getenv("DABAO_E2E") == "" {
		t.Skip("端到端测试默认跳过")
	}
	py := `C:/Users/xiong/AppData/Local/Programs/Python/Python312/python.exe`
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "main.py"), []byte("import zzz_missing_pkg\nimport helpers\nprint('x')\n"), 0o644)
	os.WriteFile(filepath.Join(proj, "helpers.py"), []byte("import zzz_inner_missing\nNAME='h'\n"), 0o644)
	cfg := &config.BuildConfig{
		EntryScript: filepath.Join(proj, "main.py"),
		PythonPath:  py,
		Name:        "warnreal",
		OneFile:     false,
	}
	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	var logs []string
	if err := ws.Run(cfg, func(l string) { logs = append(logs, l) }, nil); err != nil {
		t.Fatalf("打包失败: %v", err)
	}
	tail := strings.Join(logs[len(logs)-8:], "\n")
	t.Logf("构建日志末尾：\n%s", tail)
	if !strings.Contains(tail, "zzz_missing_pkg") || !strings.Contains(tail, "zzz_inner_missing") {
		t.Error("构建日志里应提示产物缺少这两个模块")
	}
}
