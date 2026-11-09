package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 依赖分类的准确率回归测试，针对两类真实投诉：
//  1. 本地依赖被当成独立的 pip 包（典型的 sys.path.append('lib') 后的 import）
//  2. 该发现的依赖被漏掉（lib/ 这类目录里的文件不在 PyInstaller 依赖图里）
//
// 同时确认 tests/、docs/ 里的 import 不会污染结果。
//
// 需要本机装有 PyInstaller（没有则跳过）。可用 DABAO_PYTHON 指定解释器。
func TestAnalyzeClassifiesLocalAndThirdParty(t *testing.T) {
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
	write("main.py", strings.Join([]string{
		"import os, sys",
		"sys.path.insert(0, os.path.join(os.path.dirname(__file__), 'lib'))",
		"import helpers",         // 本地模块（lib/ 注入）——不能报成 pip 包
		"from app import window", // 本地包——不能报成 pip 包
		"import zzz_missing_pkg", // 真缺失——必须报
		"import fitz",            // 缺失且模块名≠包名——必须映射成 PyMuPDF
		"try:",                   // 可选依赖：缺了会自动降级——不能报
		"    import zzz_optional_pkg",
		"except ImportError:",
		"    zzz_optional_pkg = None",
		"print(helpers.NAME, window.TITLE)",
	}, "\n"))
	write("lib/helpers.py", "import zzz_lib_dep\nNAME = 'helper'\n") // 注入目录里的依赖——必须报
	write("app/__init__.py", "")
	write("app/window.py", "TITLE = 'win'\n")
	write("tests/test_x.py", "import zzz_test_only\n") // 测试目录——不能报
	write("docs/conf.py", "import zzz_docs_only\n")    // 文档目录——不能报

	r, err := Analyze(filepath.Join(proj, "main.py"), py, nil)
	if err != nil {
		if strings.Contains(err.Error(), "No module named 'PyInstaller'") {
			t.Skip("本机 Python 未安装 PyInstaller")
		}
		t.Fatalf("分析失败: %v", err)
	}

	// 收集所有出现过的包名（missing + resolved + 间接）
	seen := map[string]bool{}
	for _, d := range r.Missing {
		seen[strings.ToLower(d.PkgName)] = true
	}
	for _, d := range r.Resolved {
		seen[strings.ToLower(d.PkgName)] = true
	}
	for _, d := range r.HookExtra {
		seen[strings.ToLower(d.PkgName)] = true
	}

	mustHave := []string{"zzz_missing_pkg", "zzz_lib_dep", "pymupdf"}
	for _, p := range mustHave {
		if !seen[p] {
			t.Errorf("漏报依赖 %q（应出现在缺失清单里）", p)
		}
	}
	// fitz 只是模块名，PyPI 上并不存在可用的 fitz 包，必须映射成 PyMuPDF
	mustNotHave := []string{"helpers", "app", "zzz_test_only", "zzz_docs_only",
		"zzz_optional_pkg", "fitz"}
	for _, p := range mustNotHave {
		if seen[p] {
			t.Errorf("误报 %q：本地模块/测试目录里的 import 不应成为依赖", p)
		}
	}

	// 缺失项必须是"未安装"的，已装的不能出现在缺失里
	for _, d := range r.Missing {
		if d.Installed {
			t.Errorf("已安装的 %q 被标记为缺失", d.PkgName)
		}
	}
}
