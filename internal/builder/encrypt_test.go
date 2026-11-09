package builder

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCollectProjectPyFiles 加密前收集用户 .py 的规则：
//   - 固定噪音目录（build/dist/__pycache__ 等）跳过
//   - 任意命名的虚拟环境按 pyvenv.cfg 识别并跳过
//   - 目录名以 env 开头的普通代码目录（environment/envs…）必须照常收集
//
// 之前用 strings.HasPrefix(name, "env") 猜虚拟环境，会把用户的 environment/ 整个跳过，
// 那些文件既不加密也不进加密模块树，等于"以为加密了其实没有"。
func TestCollectProjectPyFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(p, []byte("print(1)\n"), 0o644); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	// 应该被收集
	write("main.py")
	write("environment/settings.py") // 名字以 env 开头，但是用户代码
	write("envs/helper.py")
	write("pkg/__init__.py")

	// 应该被跳过
	write("build/generated.py")
	write("dist/output.py")
	write("__pycache__/junk.py")
	write("myenv/pyvenv.cfg") // 真虚拟环境（任意命名）
	write("myenv/Lib/site-packages/pkg/mod.py")

	files, err := collectProjectPyFiles(root)
	if err != nil {
		t.Fatalf("收集失败: %v", err)
	}
	var got []string
	for _, f := range files {
		got = append(got, filepath.ToSlash(f))
	}
	sort.Strings(got)

	want := []string{"environment/settings.py", "envs/helper.py", "main.py", "pkg/__init__.py"}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("收集结果不符\n期望: %v\n实际: %v", want, got)
	}
}
