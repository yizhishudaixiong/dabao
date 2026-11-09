package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pypacker/internal/config"
)

// 只报「被用户自己的代码引用、且不是可选导入」的缺失模块；
// 平台条件模块、第三方库内部引用、try/except 里的可选导入都不该打扰用户。
func TestMissingModulesReport(t *testing.T) {
	ws := t.TempDir()
	proj := t.TempDir()
	for _, f := range []string{"main.py", "helpers.py"} {
		if err := os.WriteFile(filepath.Join(proj, f), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	warnDir := filepath.Join(ws, "build", "app")
	if err := os.MkdirAll(warnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(proj, "main.py")
	content := strings.Join([]string{
		"missing module named zzz_missing_pkg - imported by " + entry + " (top-level)",
		"missing module named zzz_inner_missing - imported by helpers (top-level)",
		"missing module named pwd - imported by shutil (delayed, optional), tarfile (optional)",
		"missing module named zzz_optional_only - imported by main (delayed, optional)",
		"missing module named thirdparty_internal - imported by requests (top-level)",
		"excluded module named _frozen_importlib - imported by importlib (optional)",
	}, "\n")
	if err := os.WriteFile(filepath.Join(warnDir, "warn-app.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := missingModulesReport(ws, &config.BuildConfig{EntryScript: entry})
	t.Logf("报告内容：\n%s", got)

	for _, want := range []string{"zzz_missing_pkg", "zzz_inner_missing", "main.py", "helpers"} {
		if !strings.Contains(got, want) {
			t.Errorf("报告里应包含 %q", want)
		}
	}
	for _, bad := range []string{"pwd", "zzz_optional_only", "thirdparty_internal", "_frozen_importlib"} {
		if strings.Contains(got, bad) {
			t.Errorf("报告里不应出现 %q", bad)
		}
	}
}

// 没有 warn 文件（或没有命中）时返回空串，构建日志不加噪音
func TestMissingModulesReportEmpty(t *testing.T) {
	ws := t.TempDir()
	proj := t.TempDir()
	entry := filepath.Join(proj, "main.py")
	if err := os.WriteFile(entry, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := missingModulesReport(ws, &config.BuildConfig{EntryScript: entry}); got != "" {
		t.Errorf("没有 warn 文件时应返回空串，实际：%q", got)
	}
}
