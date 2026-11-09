package env

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestScanTree 锁定全盘扫描的判定语义（这些规则一旦改错，用户就会"搜不到已装的 Python"）：
//   - 盘符根目录、任意层级目录下的 python.exe 都能发现
//   - 含 pyvenv.cfg 的目录按虚拟环境处理（取 Scripts/bin 下的 python.exe，且不再深入）
//   - 命中 skipDriveDirs 的目录跳过
//   - 超过 driveMaxDepth 层不再深入
func TestScanTree(t *testing.T) {
	root := t.TempDir()
	py := pythonExeName()

	mk := func(rel string) string {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(p, []byte("fake python"), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		return p
	}
	// nested(n) 生成 d1/d2/.../dn/python.exe（所在目录位于第 n 层）
	nested := func(n int) string {
		parts := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			parts = append(parts, fmt.Sprintf("d%d", i))
		}
		return strings.Join(parts, "/") + "/" + py
	}

	want := []string{
		mk(py),                    // 盘符根目录
		mk("installed/" + py),     // 常见安装位置（第 1 层）
		mk("green/app/" + py),     // 非标准位置（第 2 层）
		mk(nested(driveMaxDepth)), // 刚好在深度上限内
	}
	// 虚拟环境：.venv 是点开头目录，但含 pyvenv.cfg，必须优先识别
	venvPy := mk("proj/.venv/Scripts/" + py)
	mk("proj/.venv/pyvenv.cfg")
	want = append(want, venvPy)

	// 以下都不应被发现
	mk("node_modules/pkg/" + py)  // 跳过名单
	mk(nested(driveMaxDepth + 1)) // 超出深度限制
	mk("dist/" + py)              // 跳过名单

	got := scanTree(root)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("命中数量不符\n期望 %d 个: %v\n实际 %d 个: %v", len(want), want, len(got), got)
	}
	for i := range want {
		if !samePath(got[i], want[i]) {
			t.Fatalf("扫描结果不符\n期望: %v\n实际: %v", want, got)
		}
	}
}

// samePath 比较路径是否指向同一位置（Windows 下大小写不敏感）
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
