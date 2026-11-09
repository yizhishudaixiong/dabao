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

// 资源打包端到端回归：验证「勾选的资源在产物里能被程序按原路径读到」。
//
// 背景（真踩过的坑）：PyInstaller 的 --add-data DEST 语义是"目标目录"，
// 文件名由它自己追加（见 PyInstaller/building/utils.py 的 format_binaries_and_datas）。
// 如果给单文件传了带文件名的 DEST，产物里就会变成 "<文件名>/<文件名>"，
// 程序按原路径 open() 直接失败。
//
//	DABAO_E2E=1 go test ./internal/builder/ -run TestResourcePackedPaths -v
func TestResourcePackedPaths(t *testing.T) {
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
	// 资源根是 sys._MEIPASS（PyInstaller 6 的 onedir 把它指向 _internal，
	// onefile 指向临时解包目录）；cwd 不等于资源根，所以必须按 _MEIPASS 解析。
	write("main.py", strings.Join([]string{
		"import os, sys",
		"base = getattr(sys, '_MEIPASS', os.path.dirname(os.path.abspath(__file__)))",
		"for p in ['config.json', 'assets/logo.png', 'data/a.txt']:",
		"    with open(os.path.join(base, p), 'rb') as f:",
		"        print(p, len(f.read()))",
		"print('OK')",
	}, "\n"))
	write("config.json", "{}")
	write("assets/logo.png", "png")
	write("data/a.txt", "a")

	res := []config.ResourceItem{
		{Path: filepath.Join(proj, "config.json"), Enabled: true, IsDir: false},
		{Path: filepath.Join(proj, "assets", "logo.png"), Enabled: true, IsDir: false},
		{Path: filepath.Join(proj, "data"), Enabled: true, IsDir: true},
	}
	cfg := &config.BuildConfig{
		EntryScript: filepath.Join(proj, "main.py"),
		PythonPath:  py,
		Name:        "probe_res_" + time.Now().Format("150405"),
		OneFile:     true,
		NoConsole:   false,
		Resources:   res,
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
	// 资源都勾选了，构建前的资源核对不应打扰用户
	if strings.Contains(strings.Join(logs, "\n"), "[资源检查]") {
		t.Errorf("资源已全部勾选时不应出现资源检查提示")
	}
	exe := filepath.Join(ws.Root, "dist", cfg.Name+".exe")
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	out, err := cmd.CombinedOutput()
	t.Logf("运行输出: %s (err=%v)", strings.TrimSpace(string(out)), err)
	if err != nil || !strings.Contains(string(out), "OK") {
		t.Errorf("【BUG】勾选的资源没有按原路径进包，程序读不到")
	}
}
