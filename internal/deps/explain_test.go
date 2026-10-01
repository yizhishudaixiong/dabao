package deps

import (
	"strings"
	"testing"
)

func TestExplainPipError(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		expect string
	}{
		{"网络超时", "Read timed out.\nCould not fetch URL https://pypi...", "网络连接失败"},
		{"连接中断", "WARNING: Retrying ... after connection broken by 'ProxyError('Cannot connect to proxy.', ConnectionResetError(10054, ...))'", "连接被中断"},
		{"包不存在", "ERROR: Could not find a version that satisfies the requirement torchx (from versions: none)", "找不到该依赖"},
		{"哈希不符", "ERROR: THESE PACKAGES DO NOT MATCH THE HASHES FROM THE REQUIREMENTS FILE", "校验值不符"},
		{"版本冲突", "ERROR: Cannot install numpy==2.0 and pandas==3.0 because these package versions have conflicting dependencies", "版本冲突"},
		{"依赖解析器冲突", "ERROR: pip's dependency resolver does not currently take into account all the packages that are installed", "版本冲突"},
		{"权限被拒", "PermissionError: [WinError 5] Access is denied", "没有写入权限"},
		{"磁盘不足", "ERROR: Could not install packages due to an OSError: [Errno 28] No space left on device", "磁盘空间不足"},
		{"编译失败", "error: subprocess-exited-with-error ... Building wheel for psycopg2 ... fatal error: Python.h: No such file or directory", "编译"},
		{"文件损坏", "zipfile.BadZipFile: File is not a zip file", "文件损坏"},
		{"归档格式异常", "ERROR: Cannot unpack file C:\\Users\\temp\\simple (downloaded from ...); cannot detect archive format", "损坏"},
		{"内存不足", "MemoryError: Unable to allocate 100 MiB", "内存不足"},
		{"旧包不兼容", "error: legacy-install-failure ... ModuleNotFoundError: No module named 'distutils'", "不兼容"},
		{"pip缺失", "python: No module named pip", "pip 组件"},
		{"依赖名格式错误", "ERROR: Invalid requirement: 'numpy=='", "格式不合法"},
		{"平台架构不匹配", "ERROR: xxx-1.0-cp39-cp39-win_amd64.whl is not a supported wheel on this platform", "预编译安装包"},
		{"SSL证书", "SSL: CERTIFICATE_VERIFY_FAILED certificate verify failed", "SSL 证书"},
		{"未知错误", "some random pip error 42", "未能自动识别"},
	}
	for _, c := range cases {
		h := ExplainPipError(c.out)
		if !strings.Contains(h.Reason, c.expect) {
			t.Errorf("[%s] 原因不匹配: got %q, want contains %q", c.name, h.Reason, c.expect)
		}
		if h.Advice == "" {
			t.Errorf("[%s] 建议为空", c.name)
		}
	}
}

func TestBuildPipDiagnosis(t *testing.T) {
	d := BuildPipDiagnosis("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12")
	if !strings.Contains(d, "依赖安装诊断") {
		t.Error("缺少诊断标题")
	}
	if !strings.Contains(d, "line12") {
		t.Error("应包含最后一行")
	}
	if strings.Contains(d, "line1\n") {
		t.Error("最近输出应只含最后10行")
	}
}
