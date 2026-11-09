package builder

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 程序名称会参与产物文件路径拼接，必须挡住路径分隔符、".." 与 Windows 非法字符，
// 否则产物可能被写到用户选择的输出目录之外。
func TestValidateAppName(t *testing.T) {
	bad := []string{
		"", " app", "app ", ".", "..",
		"a/b", `a\b`, "../evil", `..\evil`,
		"app:1", "app*", `app"`, "app?", "app<", "app>", "app|",
		"CON", "con.txt", "NUL", "COM1", "lpt9.log",
		"a\x01b",
	}
	for _, n := range bad {
		if err := validateAppName(n); err == nil {
			t.Errorf("应拒绝程序名称 %q", n)
		}
	}

	good := []string{"MyApp", "我的程序", "app-1.0", "Python打包工具", "a b", "COM10"}
	for _, n := range good {
		if err := validateAppName(n); err != nil {
			t.Errorf("应接受程序名称 %q，实际报错：%v", n, err)
		}
	}
}

// 加密打包必须把项目内所有本地模块（含子包里的每个模块）都补成 hidden-import：
// PyInstaller 从加密脚本里读不到 import，导入图是断的，只补顶层会导致
// `from pkg import widgets` 在运行时报 ImportError。
func TestCollectLocalModules(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.py") // 入口本身不应出现在清单里
	write("helpers.py")
	write("pkg/__init__.py")
	write("pkg/widgets.py")
	write("pkg/sub/__init__.py")
	write("pkg/sub/deep.py")
	write("my-script.py")      // 不是合法模块名，无法 hidden-import
	write("tests/test_x.py")   // 没有 __init__.py，不是包
	write("venv/pyvenv.cfg")   // 虚拟环境
	write("venv/lib/thing.py") // 虚拟环境里的文件不应进清单
	write("__pycache__/x.py")  // 缓存目录

	got := collectLocalModules(dir, "main")
	want := []string{"helpers", "pkg", "pkg.widgets", "pkg.sub", "pkg.sub.deep"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("模块清单不符\n实际: %v\n期望: %v", got, want)
	}
}

// 命令行过长时必须优先丢掉 --copy-metadata，绝不能丢 hidden-import / collect-all
func TestFitCmdline(t *testing.T) {
	args := []string{"-m", "PyInstaller", "--noconfirm"}
	for i := 0; i < 900; i++ {
		args = append(args, "--copy-metadata", "some-very-long-package-name-"+strconv.Itoa(i))
	}
	args = append(args, "--hidden-import", "pkg.widgets", "--collect-all", "numpy")
	if cmdlineLen(args) <= maxCmdlineLen {
		t.Fatal("测试数据未超过上限，用例失效")
	}

	var logs []string
	out := fitCmdline(args, func(s string) { logs = append(logs, s) })
	if n := cmdlineLen(out); n > maxCmdlineLen {
		t.Fatalf("裁剪后仍然超长: %d", n)
	}
	joined := strings.Join(out, " ")
	if strings.Contains(joined, "--copy-metadata") {
		t.Error("应优先丢弃 --copy-metadata")
	}
	if !strings.Contains(joined, "--hidden-import pkg.widgets") || !strings.Contains(joined, "--collect-all numpy") {
		t.Error("不能丢弃 hidden-import / collect-all（会导致产物缺模块）")
	}
	if len(logs) == 0 {
		t.Error("裁剪时应向构建日志说明原因")
	}
}

// 「排除模块」在加密模式下必须生效：既不能把顶层名重新补成 hidden-import，
// 也不能对整个包 collect-all（数据/二进制仍会被打进产物）
func TestExcludedModule(t *testing.T) {
	ex := []string{"torch", "matplotlib.pyplot"}
	if !excludedModule(ex, "torch") || !excludedModule(ex, "torch.nn") {
		t.Error("排除 torch 应同时挡住 torch 的子模块")
	}
	// 反过来不成立：排除子模块不能让整个父包被跳过，否则加密产物会整包缺失
	if excludedModule(ex, "matplotlib") {
		t.Error("排除 matplotlib.pyplot 不应把父包 matplotlib 也判为排除")
	}
	if !excludedModule(ex, "matplotlib.pyplot.figure") {
		t.Error("排除 matplotlib.pyplot 应挡住它的更深子模块")
	}
	if excludedModule(ex, "numpy") {
		t.Error("未排除的模块不应被挡")
	}
	if allModulesExcluded("torch, torchvision", ex) {
		t.Error("只排除了 torch，torchvision 仍需要 collect-all")
	}
	if !allModulesExcluded("torch, torch.nn", ex) {
		t.Error("全部 import 名都被排除时应跳过 collect-all")
	}
	if allModulesExcluded("", ex) {
		t.Error("空清单不应判定为全部排除")
	}
}
