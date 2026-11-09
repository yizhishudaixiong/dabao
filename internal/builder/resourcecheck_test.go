package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pypacker/internal/config"
)

func dep(ref string, exists, included bool, file string, line int) config.ResourceDep {
	return config.ResourceDep{
		Ref: ref, DiskPath: `D:/proj/` + ref, Exists: exists,
		Included: included, SourceFile: file, Line: line,
	}
}

// 只报"存在但没勾选"的；已勾选、磁盘上不存在的（多半是运行时生成）都不打扰用户
func TestUnbundledResources(t *testing.T) {
	deps := []config.ResourceDep{
		dep(`assets\logo.png`, true, false, "main.py", 12),
		dep(`assets\logo.png`, true, false, "ui.py", 88),   // 同一文件的第二处引用
		dep(`data\config.json`, true, true, "main.py", 20), // 已勾选
		dep(`data\missing.png`, false, false, "main.py", 30),
	}
	gaps := unbundledResources(deps)
	if len(gaps) != 1 {
		t.Fatalf("应合并成 1 条缺口，实际 %d 条：%+v", len(gaps), gaps)
	}
	g := gaps[0]
	if g.Ref != `assets\logo.png` {
		t.Errorf("ref 不符：%q", g.Ref)
	}
	if len(g.Srcs) != 2 || g.Srcs[0] != "main.py:12" || g.Srcs[1] != "ui.py:88" {
		t.Errorf("来源应合并并按字典序排列，实际：%v", g.Srcs)
	}

	msg := formatResourceGap(gaps)
	for _, want := range []string{"[资源检查]", `assets\logo.png`, "main.py:12", "ui.py:88", "打包模式"} {
		if !strings.Contains(msg, want) {
			t.Errorf("提示里应包含 %q，实际：\n%s", want, msg)
		}
	}
	for _, bad := range []string{"config.json", "missing.png"} {
		if strings.Contains(msg, bad) {
			t.Errorf("提示里不应出现 %q（已勾选或不存在）：\n%s", bad, msg)
		}
	}
}

// 没有缺口时不输出任何内容，构建日志不加噪音
func TestFormatResourceGapEmpty(t *testing.T) {
	if got := formatResourceGap(nil); got != "" {
		t.Errorf("无缺口时应返回空串，实际：%q", got)
	}
	// 全是已勾选 / 不存在的项也算无缺口
	gaps := unbundledResources([]config.ResourceDep{
		dep("a.png", true, true, "main.py", 1),
		dep("b.png", false, false, "main.py", 2),
	})
	if got := formatResourceGap(gaps); got != "" {
		t.Errorf("无缺口时应返回空串，实际：%q", got)
	}
}

// 真实跑一次资源分析：没勾选 → 有提示；勾选后 → 不再提示。
// 覆盖 checkUnbundledResources 的胶水逻辑（不打包，只跑分析，约 0.5 秒）。
func TestCheckUnbundledResourcesReal(t *testing.T) {
	py := os.Getenv("DABAO_PYTHON")
	if py == "" {
		py = `C:/Users/xiong/AppData/Local/Programs/Python/Python312/python.exe`
	}
	if _, err := os.Stat(py); err != nil {
		t.Skip("本机没有用于测试的 Python")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("open('assets/logo.png', 'rb')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "main.py")

	// 没勾选 → 必须提示
	msg := checkUnbundledResources(&config.BuildConfig{EntryScript: entry, PythonPath: py})
	if !strings.Contains(msg, "logo.png") || !strings.Contains(msg, "main.py:1") {
		t.Errorf("未勾选资源时应给出提示，实际：%q", msg)
	}

	// 勾选后 → 不再提示
	msg2 := checkUnbundledResources(&config.BuildConfig{
		EntryScript: entry, PythonPath: py,
		Resources: []config.ResourceItem{{Path: filepath.Join(dir, "assets", "logo.png"), Enabled: true}},
	})
	if msg2 != "" {
		t.Errorf("资源已勾选时不应提示，实际：%q", msg2)
	}
}
