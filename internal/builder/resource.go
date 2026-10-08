package builder

import (
	"os"
	"path/filepath"
	"strings"

	"pypacker/internal/config"
)

// ScanResources 列出程序目录下可作为资源一起打包的条目
// 自动跳过 Python 源码、缓存目录、虚拟环境等无需打包的内容
func ScanResources(programDir string) ([]config.ResourceItem, error) {
	items := []config.ResourceItem{}
	entries, err := os.ReadDir(programDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		path := filepath.Join(programDir, e.Name())
		if skipResource(path, e.IsDir()) {
			continue
		}
		items = append(items, config.ResourceItem{
			Path:    path,
			Enabled: true,
			IsDir:   e.IsDir(),
		})
	}
	return items, nil
}

func skipResource(path string, isDir bool) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "__pycache__", ".venv", "venv", "venv_py", "env", "build", "dist":
		return true
	}
	if isDir {
		// 任意命名的虚拟环境：根目录必有 pyvenv.cfg 标志文件
		if fi, err := os.Stat(filepath.Join(path, "pyvenv.cfg")); err == nil && !fi.IsDir() {
			return true
		}
	}
	if !isDir && strings.EqualFold(filepath.Ext(name), ".py") {
		return true
	}
	return false
}
