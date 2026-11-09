package builder

import "testing"

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
