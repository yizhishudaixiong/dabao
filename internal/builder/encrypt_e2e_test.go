package builder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pypacker/internal/config"
)

// 加密打包端到端回归测试：验证「本地包内子模块」在加密后仍能正常运行。
//
// 背景（这是一条真实踩过的坑）：PyArmor 加密后，PyInstaller 从加密脚本里读不到
// 任何 import 语句，导入图在加密模块处就断了。如果只把顶层模块补成 hidden-import，
// `from pkg import widgets` 会因为 pkg.widgets 没被收集，在产物运行时抛
// ImportError: cannot import name 'widgets' from 'pkg'。
//
// 默认跳过（要真实执行 PyInstaller + PyArmor，约 30 秒）：
//
//	DABAO_E2E=1 go test ./internal/builder/ -run TestEncryptedBuildLocalSubpackage -v
//
// 可用 DABAO_PYTHON 指定用于测试的解释器（需已装 pyinstaller 与 pyarmor）。
func TestEncryptedBuildLocalSubpackage(t *testing.T) {
	if os.Getenv("DABAO_E2E") == "" {
		t.Skip("端到端测试默认跳过（设置 DABAO_E2E=1 开启）")
	}
	py := os.Getenv("DABAO_PYTHON")
	if py == "" {
		py = `C:/Users/xiong/AppData/Local/Programs/Python/Python312/python.exe`
	}
	if _, err := os.Stat(py); err != nil {
		t.Skip("本机没有用于测试的 Python")
	}

	proj := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(proj, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 贴近真实项目：包内相对导入 + 二级嵌套子包
	write("main.py", "import helpers\nimport pkg\nprint('OK', helpers.VALUE, pkg.widgets.NAME, pkg.widgets.deep.TAG)\n")
	write("helpers.py", "VALUE = 'helper'\n")
	write("pkg/__init__.py", "from . import widgets\n")
	write("pkg/widgets.py", "from .sub import deep\nNAME = 'widget'\n")
	write("pkg/sub/__init__.py", "")
	write("pkg/sub/deep.py", "TAG = 'deep'\n")

	// 未加密 + 多文件（基线） / 加密 + 单文件（默认形态，覆盖 onefile 下的运行时释放）
	cases := []struct {
		name    string
		mode    string
		oneFile bool
	}{
		{"plain_onedir", "", false},
		{"basic_onefile", "basic", true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.BuildConfig{
				EntryScript: filepath.Join(proj, "main.py"),
				PythonPath:  py,
				Name:        "probe_" + c.name + "_" + time.Now().Format("150405"),
				OneFile:     c.oneFile,
				NoConsole:   false,
				EncryptMode: c.mode,
			}
			ws, err := NewWorkspace()
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Cleanup()
			if err := ws.Run(cfg, func(string) {}, nil); err != nil {
				t.Fatalf("打包失败: %v", err)
			}
			exe := filepath.Join(ws.Root, "dist", cfg.Name+".exe")
			if !cfg.OneFile {
				exe = filepath.Join(ws.Root, "dist", cfg.Name, cfg.Name+".exe")
			}
			if _, err := os.Stat(exe); err != nil {
				t.Fatalf("产物不存在: %v", err)
			}
			cmd := exec.Command(exe)
			cmd.Dir = filepath.Dir(exe)
			out, err := cmd.CombinedOutput()
			t.Logf("[%s] 运行输出: %s (err=%v)", c.name, strings.TrimSpace(string(out)), err)
			if err != nil || !strings.Contains(string(out), "OK helper widget deep") {
				t.Errorf("【BUG】%s 模式产物运行失败（加密后本地子模块没有被打进产物）", c.name)
			}
		})
	}
}
