package main

import (
	"path/filepath"
	"strings"
	"testing"

	"pypacker/internal/config"
	"pypacker/internal/portable"
)

// withinDir 必须按路径层级判断，不能用字符串前缀：
// "...\runtime2" 不该被当成 "...\runtime" 的子路径。
func TestWithinDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "runtime")
	inside := filepath.Join(base, "cpython-3.12.14", "python", "python.exe")
	sibling := filepath.Join(filepath.Dir(base), "runtime2", "python.exe")

	if !withinDir(inside, base) {
		t.Fatalf("%q 应判定为位于 %q 之内", inside, base)
	}
	if withinDir(sibling, base) {
		t.Fatalf("%q 不应判定为位于 %q 之内（只是名字前缀相同）", sibling, base)
	}
	if withinDir(base, base) {
		t.Fatal("目录本身不应算作自己的子路径")
	}
}

// 按路径还原/手动选择环境时，落在便携版根目录下的必须仍标记为「内置便携版」，
// 否则界面会显示成"物理环境"，用户以为环境换了。
func TestDecorateEnvMarksPortable(t *testing.T) {
	portablePy := filepath.Join(portable.RootDir(), "cpython-3.12.14", "python", "python.exe")
	e := config.PythonEnv{Path: portablePy, Version: "3.12.14"}
	decorateEnv(&e)
	if !e.IsPortable {
		t.Fatal("便携版路径未被标记 IsPortable")
	}
	if !strings.HasPrefix(e.Name, "内置便携版") {
		t.Fatalf("展示名应为内置便携版，实际 %q", e.Name)
	}

	other := config.PythonEnv{Path: filepath.Join(t.TempDir(), "Python312", "python.exe"), Version: "3.12.14"}
	decorateEnv(&other)
	if other.IsPortable {
		t.Fatal("普通安装路径被误标为便携版")
	}
	if !strings.HasPrefix(other.Name, "物理环境") {
		t.Fatalf("展示名应为物理环境，实际 %q", other.Name)
	}
}
