package main

import (
	"path/filepath"
	"strings"
	"testing"

	"pypacker/internal/config"
	"pypacker/internal/portable"
)

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
