package builder

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
)

// 资源依赖静态分析：用 Python AST 解析程序目录下所有 .py 的"读取类调用"
// （open / cv2.imread / np.load / torch.load / os.path.join / Path 等），
// 与用户已勾选的打包资源清单对比，标出「已包含 / 未包含」。
// 动态路径（变量拼接）与 Windows 系统库（如 user32.dll）不参与展示。
//
// 本功能为启发式分析：
//  - 可能误报（字符串常量被当作路径）
//  - 可能漏报（变量拼接、运行时生成的路径无法静态识别）
// 结果仅供提醒，最终以打包后运行测试为准。

//go:embed ast_scan.py
var astScanScript string

// astScanItem 是 Python 脚本输出的单条分析结果
type astScanItem struct {
	Ref      string `json:"ref"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Dynamic  bool   `json:"dynamic"`
	Kind     string `json:"kind"`
	FileBase bool   `json:"file_base"`
	Code     string `json:"code"`
}

// AnalyzeResourceDeps 用指定 Python 环境执行 AST 扫描，分析代码引用的资源与已勾选资源对比。
func AnalyzeResourceDeps(pythonPath, entryScript string, resources []config.ResourceItem) ([]config.ResourceDep, error) {
	entryDir := filepath.Dir(entryScript)

	// 1. 把内嵌扫描脚本写到临时文件
	tmp, err := os.CreateTemp("", "ast_scan_*.py")
	if err != nil {
		return nil, fmt.Errorf("创建分析脚本失败: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(astScanScript); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("写入分析脚本失败: %v", err)
	}
	tmp.Close()

	// 2. 执行脚本（找不到 python 时兜底用 python 命令）
	//    用 cmdutil.Command 封装：Windows 下隐藏控制台窗口（避免闪黑框）、强制 UTF-8 输出（避免中文乱码）
	py := pythonPath
	if py == "" {
		py = "python"
	}
	out, err := cmdutil.Command(py, tmpName, entryDir).Output()
	if err != nil {
		return nil, fmt.Errorf("资源分析执行失败: %v", err)
	}
	var items []astScanItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("资源分析结果解析失败: %v", err)
	}

	// 3. 转换为前端展示结构
	// 只保留「已包含 / 未包含」两类：
	//  - 动态路径（参数是变量，无法静态识别）→ 跳过，不展示
	//  - 系统库（DLL 纯文件名且项目目录中不存在，如 user32.dll）→ 跳过，不展示
	var deps []config.ResourceDep
	seen := map[string]bool{}
	for _, s := range items {
		if s.Dynamic {
			continue
		}
		d := config.ResourceDep{
			SourceFile: s.File,
			Line:       s.Line,
			Code:       s.Code,
		}
		key := d.SourceFile + "|" + strconv.Itoa(d.Line) + "|" + d.Ref
		if seen[key] {
			continue
		}

		ref := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s.Ref), "./"), `.\`)
		if ref == "" {
			continue
		}
		d.Ref = ref
		d.Kind = "file"

		// 路径基准：join + __file__ → 源文件所在目录；其余 → 入口脚本目录
		baseDir := entryDir
		if s.Kind == "join" && s.FileBase {
			baseDir = filepath.Join(entryDir, filepath.Dir(s.File))
		}
		abs := ref
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(baseDir, filepath.FromSlash(abs))
		}
		abs = filepath.Clean(abs)
		exists := fileExists(abs)
		// 解析出的路径不存在时，在入口目录下递归查找同名文件（变量拼路径常指到子目录）
		if !exists {
			if found := findSameName(entryDir, filepath.Base(abs)); found != "" {
				abs = found
				exists = true
			}
		}

		// join 拼接的"目录探测"处理（最后一段无扩展名，如 shu_config / _internal / ..）：
		// 这类是代码的候选搜索路径，不是要打包的资源文件。
		//  - .. 和 . 是路径跳转 → 直接忽略
		//  - 其他目录名 → 仅当该目录已被勾选时保留（提示"已包含"），否则忽略
		if s.Kind == "join" {
			refLast := lastSegmentRef(ref)
			if refLast == ".." || refLast == "." {
				continue
			}
			if filepath.Ext(refLast) == "" {
				if !resDepIncluded(resources, abs) {
					continue
				}
			}
		}

		// 系统库：DLL 加载调用 + 纯文件名 + 项目目录里找不到 → Windows 系统/外部库，不展示
		if s.Kind == "dll" && !strings.ContainsAny(ref, `/\\`) && !exists {
			continue
		}

		d.DiskPath = abs
		d.Exists = exists
		d.Included = resDepIncluded(resources, abs)
		seen[key] = true
		deps = append(deps, d)
	}
	return deps, nil
}

// lastSegmentRef 取 ref 的最后一段（含 .. / . 原样返回）
func lastSegmentRef(ref string) string {
	i := strings.LastIndexAny(strings.TrimRight(ref, `/\\`), `/\\`)
	if i == -1 {
		return ref
	}
	return ref[i+1:]
}

// findSameName 在入口目录下递归查找同名文件（跳过噪音目录），返回第一个匹配的完整路径
func findSameName(rootDir, name string) string {
	var found string
	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootDir && skipScanDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), name) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// 分析时跳过的目录（缓存 / 虚拟环境 / 构建产物等）
var skipScanDirs = map[string]bool{
	"__pycache__": true, ".venv": true, "venv": true, "env": true,
	"build": true, "dist": true, ".git": true, "node_modules": true,
	".idea": true, ".vscode": true, ".pytest_cache": true,
}

// resDepIncluded 判断绝对路径是否已被勾选的资源覆盖（精确匹配或位于已勾选文件夹内）
func resDepIncluded(resources []config.ResourceItem, abs string) bool {
	clean := filepath.Clean(abs)
	for _, r := range resources {
		if !r.Enabled {
			continue
		}
		if strings.EqualFold(filepath.Clean(r.Path), clean) {
			return true
		}
		if r.IsDir {
			rel, err := filepath.Rel(r.Path, clean)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
