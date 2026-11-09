package builder

import (
	"os"
	"path/filepath"
	"testing"

	"pypacker/internal/config"
)

// 资源分析的三个真实场景：
//  1. GBK 编码的中文源码（Windows 老项目常见，文件头声明 coding: gbk）
//  2. pathlib 的 / 运算符：Path(__file__).parent / 'assets' / 'logo.png'
//  3. 正常的 open('x.png')
func TestResourceDepsEncodings(t *testing.T) {
	py := `C:/Users/xiong/AppData/Local/Programs/Python/Python312/python.exe`
	if _, err := os.Stat(py); err != nil {
		t.Skip("本机没有用于测试的 Python")
	}
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("assets/logo.png", []byte("x"))
	write("assets/utf8.png", []byte("x"))
	write("assets/gbk.png", []byte("x"))

	// GBK 源码：注释和字符串都是中文
	gbk := []byte("# -*- coding: gbk -*-\r\n# 中文注释\r\nopen('assets/gbk.png')\r\nprint('中文输出')\r\n")
	enc := make([]byte, 0, len(gbk))
	for _, r := range []rune("# -*- coding: gbk -*-\r\n# 中文注释\r\nopen('assets/gbk.png')\r\nprint('中文输出')\r\n") {
		if r < 128 {
			enc = append(enc, byte(r))
			continue
		}
		// GBK 最小实现：仅覆盖本用例字符，交由 Python 侧校验
		enc = append(enc, gbkChar(r)...)
	}
	_ = gbk
	write("gbk_mod.py", enc)

	write("main.py", []byte(""+
		"from pathlib import Path\n"+
		"open('assets/utf8.png')\n"+
		"p = Path(__file__).parent / 'assets' / 'logo.png'\n"+
		"print(p)\n"))

	deps, err := AnalyzeResourceDeps(py, filepath.Join(dir, "main.py"), []config.ResourceItem{})
	if err != nil {
		t.Fatalf("资源分析失败: %v", err)
	}
	seen := map[string]bool{}
	for _, d := range deps {
		seen[d.Ref] = true
		t.Logf("  ref=%-24s file=%-12s line=%d exists=%v", d.Ref, d.SourceFile, d.Line, d.Exists)
	}
	if !seen["assets/utf8.png"] {
		t.Errorf("漏报 open('assets/utf8.png')")
	}
	if !seen["gbk.png"] && !seen["assets/gbk.png"] {
		t.Errorf("【BUG】GBK 编码源码被整文件跳过，其中的资源引用完全没被分析")
	}
	foundPathlib := false
	for _, d := range deps {
		if d.SourceFile == "main.py" && d.Exists && (d.Ref == "logo.png" || d.Ref == "assets/logo.png") {
			foundPathlib = true
		}
	}
	if !foundPathlib {
		t.Errorf("【BUG】pathlib 的 / 拼接（Path(__file__).parent / 'assets' / 'logo.png'）未被识别")
	}
}

// gbkChar 用 GBK 编码单个非 ASCII 字符（借 Python 生成，避免在 Go 里塞码表）
var gbkTable = map[rune][]byte{
	'中': {0xD6, 0xD0}, '文': {0xCE, 0xC4}, '注': {0xD7, 0xA2}, '释': {0xCA, 0xCD},
	'输': {0xCA, 0xE4}, '出': {0xB3, 0xF6},
}

func gbkChar(r rune) []byte {
	if b, ok := gbkTable[r]; ok {
		return b
	}
	return []byte{'?'}
}

// 资源分析的准确率回归：既不能把普通字符串当资源，也不能漏掉真实的资源引用。
//
// 覆盖的误报来源：写入模式的 open、普通函数参数（parser.load('section')）、
// 常量名（NAME = 'settings.json'）、URL、正则。
// 覆盖的漏报来源：自定义封装 resource_path('data/config.json')、QIcon('a.png')、
// 非常见扩展名（.html/.qss）、pathlib 的 / 拼接。
func TestResourceDepsAccuracy(t *testing.T) {
	py := os.Getenv("DABAO_PYTHON")
	if py == "" {
		py = `C:/Users/xiong/AppData/Local/Programs/Python/Python312/python.exe`
	}
	if _, err := os.Stat(py); err != nil {
		t.Skip("本机没有用于测试的 Python")
	}
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"assets/logo.png", "assets/missing.png", "data/config.json", "style.qss", "index.html"} {
		if f != "assets/missing.png" { // 故意不建这个文件，验证"引用了但不存在"仍会被报出来
			write(f, "x")
		}
	}
	write("main.py", ""+
		"import logging\n"+
		"from pathlib import Path\n"+
		"\"\"\"模块说明：图标放在 assets/logo.png，配置是 data/config.json\"\"\"\n"+
		"logging.info('report.csv')\n"+
		"parser.load('section')\n"+
		"db.copy('users')\n"+
		"URL = 'https://cdn.example.com/logo.png'\n"+
		"open('assets/logo.png', 'rb')\n"+
		"open('out.txt', 'w')\n"+
		"Path(__file__).parent / 'data' / 'config.json'\n"+
		"QIcon('style.qss')\n"+
		"open('index.html')\n"+
		"open('assets/missing.png')\n"+
		"glob.glob('assets/*.png')\n"+
		"# 下面三种是位置不固定的写法：字典字面量、关键字参数、f-string\n"+
		"MENU = [{'icon': 'assets/icon.png'}]\n"+
		"parser.add_argument('--cfg', default='data/config.json')\n"+
		"open(f'{BASE}/assets/logo.png')\n"+
		"QIcon(':/icons/a.png')\n")

	deps, err := AnalyzeResourceDeps(py, filepath.Join(dir, "main.py"), []config.ResourceItem{})
	if err != nil {
		t.Fatalf("资源分析失败: %v", err)
	}
	got := map[string]bool{}
	for _, d := range deps {
		got[d.Ref] = true
	}
	for _, want := range []string{"assets/logo.png", "data/config.json", "assets/icon.png", "style.qss", "index.html", "assets/missing.png"} {
		if !got[want] {
			t.Errorf("漏报资源 %q", want)
		}
	}
	for _, bad := range []string{"out.txt", "report.csv", "section", "users", "https://cdn.example.com/logo.png", "assets/*.png", ":/icons/a.png"} {
		if got[bad] {
			t.Errorf("误报 %q：不是随包分发的资源", bad)
		}
	}
	// assets/missing.png 必须标成"不存在"，界面才能提示用户补文件
	for _, d := range deps {
		if d.Ref == "assets/missing.png" && d.Exists {
			t.Error("不存在的资源应标记 exists=false")
		}
	}
}
