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
	var items []config.ResourceItem
	entries, err := os.ReadDir(programDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if skipResource(name, e.IsDir()) {
			continue
		}
		items = append(items, config.ResourceItem{
			Path:    filepath.Join(programDir, name),
			Enabled: true,
			IsDir:   e.IsDir(),
		})
	}
	return items, nil
}

func skipResource(name string, isDir bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "__pycache__", ".venv", "venv", "env", "build", "dist":
		return true
	}
	if !isDir && strings.EqualFold(filepath.Ext(name), ".py") {
		return true
	}
	return false
}
