package builder

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/errhint"
	"pypacker/internal/fsx"
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
	// 跳过目录清单从 Go 侧传入：与 env / builder 共用 internal/fsx 里的同一份定义，
	// 不再由 Python 脚本单独维护（两份清单迟早会走样）
	skipJSON, _ := json.Marshal(fsx.NoiseDirNames())
	out, err := cmdutil.Command(py, tmpName, entryDir, string(skipJSON)).Output()
	if err != nil {
		// Output() 会把子进程 stderr 收进 ExitError：脚本崩了（语法不兼容、
		// 目标解释器版本过低等）时，把 traceback 带出来，否则界面上只剩
		// 一句"exit status 1"，用户完全无法自查。
		var ee *exec.ExitError
		if errors.As(err, &ee) && strings.TrimSpace(string(ee.Stderr)) != "" {
			return nil, fmt.Errorf("资源分析执行失败: %v\n%s", err, errhint.Tail(string(ee.Stderr), 12))
		}
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
	// 文件名 -> 完整路径索引，首次需要时才建立（大多数项目用不到）
	var nameIndex map[string]string
	for _, s := range items {
		if s.Dynamic {
			continue
		}
		d := config.ResourceDep{
			SourceFile: s.File,
			Line:       s.Line,
			Code:       s.Code,
		}
		// 去重键必须带上原始引用（s.Ref）：同一行可能引用多个资源
		// （如 os.path.join('a.txt', 'b.txt')），只用"文件+行号"会误删后面的引用
		key := s.File + "|" + strconv.Itoa(s.Line) + "|" + s.Ref
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
		// 解析出的路径不存在时，在入口目录下找同名文件（变量拼路径常指到子目录）。
		// 用一次性建立的索引，避免每个未命中引用都遍历一遍整个项目目录。
		if !exists {
			if nameIndex == nil {
				nameIndex = buildNameIndex(entryDir)
			}
			if found, ok := nameIndex[strings.ToLower(filepath.Base(abs))]; ok {
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
	// 空结果返回空切片而非 nil：nil 经 Wails 序列化成 JSON null，
	// 前端直接读 list.length 会崩溃（Cannot read properties of null）
	if deps == nil {
		deps = []config.ResourceDep{}
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

// buildNameIndex 递归建立「文件名（小写）→ 完整路径」索引（跳过噪音目录与虚拟环境）。
// 同名文件取遍历顺序里的第一个，与逐次遍历的结果一致，但整体只走一遍目录树。
func buildNameIndex(rootDir string) map[string]string {
	index := map[string]string{}
	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootDir {
				if fsx.IsNoiseDir(d.Name()) {
					return filepath.SkipDir
				}
				// 虚拟环境按 pyvenv.cfg 识别（任意命名都能跳过），避免在第三方库里误找同名资源
				if fsx.HasPyvenvCfg(path) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		key := strings.ToLower(d.Name())
		if _, ok := index[key]; !ok {
			index[key] = path
		}
		return nil
	})
	return index
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
		if r.IsDir && fsx.IsWithin(clean, r.Path) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
