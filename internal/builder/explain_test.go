package builder

import (
	"strings"
	"testing"
)

func TestExplainBuildError(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		expect  string
		notWant string
	}{
		{"缺模块", "Traceback...\nModuleNotFoundError: No module named 'numpy'\n[1234] Failed to execute script", "缺少第三方依赖", ""},
		{"缺模块提取名", "ModuleNotFoundError: No module named 'win32gui'", "win32gui", ""},
		{"环境损坏", "ModuleNotFoundError: No module named 'encodings'", "环境已损坏", ""},
		{"语法错误", "File \"main.py\", line 3\n    print(1 +\nSyntaxError: invalid syntax", "语法错误", ""},
		{"4GB限制", "struct.error: 'I' format requires 0 <= number <= 4294967295", "4GB", ""},
		{"递归超限", "RecursionError: maximum recursion depth exceeded", "递归深度超限", ""},
		{"多进程", "An attempt has been made to start a new process before the current process has finished its bootstrapping phase", "多进程", ""},
		{"Qt冲突", "Aborting build process due to attempt to collect multiple Qt bindings packages: attempting to run hook for 'PyQt5', while hook for 'PySide6' has already been run", "PyQt 和 PySide", ""},
		{"Qt插件缺失", "Could not find the Qt platform plugin \"windows\"", "平台插件", ""},
		{"VC运行库缺失", "lib not found: api-ms-win-cr-runtime-l1-1-0.dll", "Visual C++ 运行库", ""},
		{"加密冲突", "RuntimeError: unauthorized use of script (1:5174)", "加密", ""},
		{"库收集不完整", "ImportError: cannot import name '_util'", "没有被完整收集", ""},
		{"Python库找不到", "IOError: Python library not found!", "Python 核心库", ""},
		{"元数据缺失", "pkg_resources.DistributionNotFound: The 'cryptography' distribution was not found", "元数据", ""},
		{"PyInstaller版本不兼容", "Your system is not supported. PyInstaller requires at least Python 3.8", "不兼容", ""},
		{"UPX失败", "Error: UPX is not available. Install UPX or use --noupx", "UPX", ""},
		{"Bootloader缺失", "Fatal error: PyInstaller does not include a pre-compiled bootloader for your platform", "引导程序", ""},
		{"位数不匹配", "is not a valid Win32 application", "位数", ""},
		{"重复收集", "WARNING: file already exists but should not: _C.cp37-win_amd64", "重复收集", ""},
		{"权限被拒", "PermissionError: [Errno 13] Permission denied: 'dist\\\\app.exe'", "权限", ""},
		{"内存不足", "MemoryError: Unable to allocate", "内存", ""},
		{"OpenCV配置缺失", "ImportError: OpenCV loader: missing configuration file: ['config.py']", "OpenCV", ""},
		{"未知错误", "some weird error 12345", "未能自动识别", ""},
	}
	for _, c := range cases {
		hint := ExplainBuildError(c.out)
		if !strings.Contains(hint.Reason, c.expect) {
			t.Errorf("[%s] 原因不匹配: got %q, want contains %q", c.name, hint.Reason, c.expect)
		}
		if c.notWant != "" && strings.Contains(hint.Reason, c.notWant) {
			t.Errorf("[%s] 原因误报: got %q, should not contain %q", c.name, hint.Reason, c.notWant)
		}
	}
	// 兜底用例应给出建议
	h := ExplainBuildError("weird 123")
	if h.Advice == "" {
		t.Error("兜底建议为空")
	}
}

func TestBuildDiagnosisText(t *testing.T) {
	out := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12"
	d := BuildDiagnosisText(ExplainBuildError("x"), out)
	if !strings.Contains(d, "打包失败诊断") {
		t.Error("缺少诊断标题")
	}
	if !strings.Contains(d, "line3") {
		t.Error("最近输出应只含最后10行，line3 不应出现")
	}
	if !strings.Contains(d, "line12") {
		t.Error("最近输出应包含最后一行")
	}
}
