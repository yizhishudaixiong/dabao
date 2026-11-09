// Package fsx 收敛文件系统相关的通用判断。
//
// 这些规则原先在 env / builder / deps 里各写了一份（跳过目录清单有 5 份、
// 路径归属判断有 3 份），改一处忘一处就会出问题——比如"用名字前缀猜虚拟环境"
// 那个 bug 就是把一套规则单独维护跑偏的结果。统一到这里后，语义只有一个出处。
package fsx

import (
	"os"
	"path/filepath"
	"strings"
)

// IsWithin 判断 child 是否位于 parent 之内（child == parent 时也算）。
// 按路径层级判断，不能用字符串前缀：否则 "C:\dabao\runtime2" 会被误判为
// "C:\dabao\runtime" 的子路径。
func IsWithin(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// HasPyvenvCfg 判断目录是否为 Python 虚拟环境根目录。
// Python 3.3+ 创建的虚拟环境根目录必有 pyvenv.cfg，用它识别比按目录名猜可靠得多：
// venv_py / .venv / myenv 这类任意命名都能识别，也不会把用户的 environment/ 误当成环境。
func HasPyvenvCfg(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "pyvenv.cfg"))
	return err == nil && !fi.IsDir()
}

// noiseDirs 扫描项目目录时一律跳过的"噪音"目录：版本控制、IDE、缓存、构建产物、依赖目录。
// 在这些目录里找 .py / 资源没有意义，只会拖慢扫描并产生误报。
var noiseDirs = map[string]bool{
	"__pycache__": true,
	".git":        true,
	".svn":        true,
	".hg":         true,
	".venv":       true,
	"venv":        true,
	"venv_py":     true,
	// 注意：这里不能放 "env"。它是很常见的用户包名/目录名（env/__init__.py），
	// 而真正的虚拟环境（含名为 env 的）都有 pyvenv.cfg，由 HasPyvenvCfg 识别，
	// 不需要再按名字猜——按名字猜就会把用户代码当成虚拟环境跳过（漏加密/漏扫描）。
	"site-packages": true,
	"node_modules":  true,
	".idea":         true,
	".vscode":       true,
	".pytest_cache": true,
	"build":         true,
	"dist":          true,
}

// systemJunkDirs 只有全盘扫描才会遇到的系统/回收站目录，项目目录扫描不必考虑。
var systemJunkDirs = map[string]bool{
	"windows":                   true,
	"$recycle.bin":              true,
	"system volume information": true,
	"recovery":                  true,
}

// IsNoiseDir 目录名是否属于项目内扫描应跳过的噪音目录（大小写不敏感，符合 Windows 语义）。
func IsNoiseDir(name string) bool {
	return noiseDirs[strings.ToLower(name)]
}

// IsSystemJunkDir 目录名是否属于全盘扫描额外要跳过的系统目录。
func IsSystemJunkDir(name string) bool {
	return systemJunkDirs[strings.ToLower(name)]
}

// NoiseDirNames 返回噪音目录名列表（供 Python 脚本复用同一份清单，避免两边各写一份）。
func NoiseDirNames() []string {
	out := make([]string, 0, len(noiseDirs))
	for name := range noiseDirs {
		out = append(out, name)
	}
	return out
}
