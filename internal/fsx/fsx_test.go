package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

// IsWithin 必须按路径层级判断，不能用字符串前缀：
// "...\runtime2" 不该被当成 "...\runtime" 的子路径。
func TestIsWithin(t *testing.T) {
	base := filepath.Join(t.TempDir(), "runtime")
	inside := filepath.Join(base, "cpython-3.12.14", "python", "python.exe")
	sibling := filepath.Join(filepath.Dir(base), "runtime2", "python.exe")

	if !IsWithin(inside, base) {
		t.Fatalf("%q 应判定为位于 %q 之内", inside, base)
	}
	if IsWithin(sibling, base) {
		t.Fatalf("%q 不应判定为位于 %q 之内（只是名字前缀相同）", sibling, base)
	}
	if !IsWithin(base, base) {
		t.Fatal("目录本身应算作自己的子路径")
	}
	if IsWithin("", base) || IsWithin(base, "") {
		t.Fatal("空路径一律返回 false")
	}
}

// 虚拟环境按 pyvenv.cfg 识别，与目录名无关
func TestHasPyvenvCfg(t *testing.T) {
	dir := t.TempDir()
	venv := filepath.Join(dir, "my-environment") // 名字里不含 venv，也必须能识别
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	if HasPyvenvCfg(venv) {
		t.Fatal("没有 pyvenv.cfg 时不应判定为虚拟环境")
	}
	if err := os.WriteFile(filepath.Join(venv, "pyvenv.cfg"), []byte("home = x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasPyvenvCfg(venv) {
		t.Fatal("含 pyvenv.cfg 时应判定为虚拟环境")
	}
}

// 噪音目录清单大小写不敏感（Windows 语义），系统目录单独一组
func TestNoiseDirNames(t *testing.T) {
	for _, name := range []string{"__pycache__", ".GIT", "Node_Modules", "build", "env", "venv_py"} {
		if !IsNoiseDir(name) {
			t.Errorf("应判定为噪音目录: %q", name)
		}
	}
	for _, name := range []string{"src", "assets", "package", "environment"} {
		if IsNoiseDir(name) {
			t.Errorf("不应判定为噪音目录: %q", name)
		}
	}
	if !IsSystemJunkDir("Windows") || !IsSystemJunkDir("$Recycle.Bin") {
		t.Error("系统目录应大小写不敏感地命中")
	}
	if IsSystemJunkDir("Python312") {
		t.Error("普通目录不应命中系统目录清单")
	}
	if len(NoiseDirNames()) == 0 {
		t.Error("NoiseDirNames 不应为空（Python 脚本要靠它复用同一份清单）")
	}
}
