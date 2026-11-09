package deps

import (
	"bufio"
	"crypto/md5"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/env"
	"pypacker/internal/paths"
)

//go:embed pipreqs_mapping.txt
var pipreqsMapping string

// pyarmorVerRe 匹配 PyArmor 版本号，如 "9.2.6" / "8.5.8"
var pyarmorVerRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// CheckInstalled 检测目标 Python 环境是否已安装指定 pip 包，返回是否已装与版本号。
// 用隐藏窗口执行，避免 GUI 内弹出黑框；版本为空且无报错时按"未安装"处理。
func CheckInstalled(pythonPath, pkg string) (bool, string) {
	script := "import sys, importlib.metadata as m\nsys.stdout.reconfigure(encoding='utf-8')\ntry:\n    print(m.version('" + pkg + "'))\nexcept Exception:\n    pass\n"
	cmd := cmdutil.Command(pythonPath, "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, ""
	}
	ver := strings.TrimSpace(string(out))
	return ver != "", ver
}

// FindPyarmor 定位所选 Python 环境中的 pyarmor 可执行文件（统一转发到 env 包实现）
func FindPyarmor(pythonPath string) string {
	return env.FindPyarmor(pythonPath)
}

// CheckPyarmor 检测目标 Python 环境是否已安装 PyArmor（代码加密用），返回是否可用及版本信息。
// 使用隐藏窗口执行，避免 GUI 内弹出黑框。
func CheckPyarmor(pythonPath string) (bool, string) {
	exe := env.FindPyarmor(pythonPath)
	if exe == "" {
		return false, ""
	}
	cmd := cmdutil.Command(exe, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, ""
	}
	// 版本信息通常在第一行，形如 "Pyarmor 9.2.6 (trial), 000000, non-profits"
	first := strings.TrimSpace(strings.Split(strings.TrimSpace(string(out)), "\n")[0])
	return true, extractPyarmorVersion(first)
}

// extractPyarmorVersion 从 "Pyarmor 9.2.6 (trial)..." 中提取纯版本号 "9.2.6"，
// 避免把许可证信息（trial/000000/non-profits）显示在界面上
func extractPyarmorVersion(s string) string {
	m := pyarmorVerRe.FindString(s)
	if m == "" {
		return s
	}
	return m
}

// analyzeScript 是内嵌的 Python 分析脚本（内容见同目录 analyze.py），
// 基于 PyInstaller 官方分析引擎完成依赖分析。
//
// 原理（比旧版"纯静态 AST 扫描"更准）：
//  1. 候选提取：先用 AST 扫描入口脚本 + 递归本地模块，收集代码里 import 的顶层模块名
//     （这是"程序引用了什么"；PyInstaller 的 missing 清单含大量平台/hook 噪音，不能直接用，
//     所以缺失判断必须以代码 import 候选为准）
//  2. 真相来源：调用 PyInstaller 官方 Analysis API（与正式打包同一引擎），它做字节码扫描、
//     递归展开依赖树、执行上千个官方 hook，输出"实际能收集到的模块"（含动态导入补充）
//  3. 差集判定：
//     - 候选命中 Analysis 收集结果 → 已安装
//     - 候选未命中、且非标准库/非本地模块 → 真缺失
//     - Analysis 收集到、但代码没直接 import 的第三方模块 → "钩子补充"（间接依赖，
//       随主包 pip 自动安装，仅展示）
//  4. analysisModules：Analysis 收集的全部第三方模块全名，供加密打包时全量喂给
//     PyInstaller 收集阶段（加密后 PyInstaller 看不到 import，必须用这份清单替它补齐）
//
// 注意：Analysis 的 INFO 日志输出到 stderr，Go 侧逐行转发做进度显示；
// stdout 只输出最后一个 JSON 行。
//go:embed analyze.py
var analyzeScript string

// PyiCacheDir 返回某入口脚本专属的 PyInstaller 分析缓存目录（工具数据目录下）。
// 按入口路径哈希分目录：每个项目的缓存互不干扰，切回老项目时命中各自缓存秒回
// （PyInstaller 自带"入口/依赖未变则复用分析结果"的缓存，无需自己实现失效逻辑）。
func PyiCacheDir(entryScript string) string {
	sum := md5.Sum([]byte(filepath.Clean(entryScript)))
	dir := filepath.Join(paths.PyiCacheRoot(), fmt.Sprintf("%x", sum)[:16])
	os.MkdirAll(dir, 0o755)
	return dir
}

// maxPyiCacheProjects 保留最近使用过的项目分析缓存数，超出自动清理最旧的，
// 防止长期使用缓存目录无限膨胀（缓存本身每个项目仅几 MB，此上限兜底）。
const maxPyiCacheProjects = 10

// CleanPyiCache 保留最近 maxPyiCacheProjects 个项目的分析缓存，删除更旧的目录。
// 每次分析结束后调用一次；删除的只是缓存，下次分析会自动重建（多等几秒）。
func CleanPyiCache() {
	root := paths.PyiCacheRoot()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) <= maxPyiCacheProjects {
		return
	}
	type item struct {
		path string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{path: filepath.Join(root, e.Name()), mod: fi.ModTime()})
	}
	if len(items) <= maxPyiCacheProjects {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	for _, it := range items[maxPyiCacheProjects:] {
		os.RemoveAll(it.path)
	}
}

// Analyze 基于 PyInstaller 官方分析引擎分析入口脚本的第三方依赖：
//   - 目标 Python 环境执行（隐藏窗口不闪黑框）
//   - onProgress 非空时，逐行转发分析引擎的实时日志（供界面做进度显示）
//   - 返回缺失/已装/钩子补充清单，以及供加密打包使用的全量第三方模块清单
func Analyze(entryScript, pythonPath string, onProgress func(string)) (*config.DepsResult, error) {
	if entryScript == "" || pythonPath == "" {
		return nil, fmt.Errorf("入口脚本和 Python 路径不能为空")
	}
	// 把 go:embed 的官方映射表注入脚本（占位符替换；映射表为纯 ASCII，无引号冲突）
	script := strings.Replace(analyzeScript, "__PIPREQS_MAPPING__", pipreqsMapping, 1)
	// 脚本含 1152 条映射，体积大：用 -c 传参会超过 Windows 命令行长度限制，
	// 改为写入临时脚本文件再执行（用完即删）。
	tmp, err := os.CreateTemp("", "deps_scan_*.py")
	if err != nil {
		return nil, fmt.Errorf("创建临时分析脚本失败: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("写入临时分析脚本失败: %v", err)
	}
	tmp.Close()

	cmd := cmdutil.Command(pythonPath, tmpName, entryScript, PyiCacheDir(entryScript))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建输出管道失败: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建错误管道失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动依赖分析失败: %v", err)
	}

	// 逐行读取 stdout/stderr：JSON 行是最终结果，其余行是分析引擎日志（转发给界面做进度）；
	// stderr 全文同时累积，供失败时拼装中文诊断（分析脚本崩溃的 traceback 一定在 stderr）
	var jsonLine string
	var errOut strings.Builder
	var mu sync.Mutex
	readPipe := func(rc io.ReadCloser, isErr bool) {
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if !isErr && strings.HasPrefix(line, "{") {
				mu.Lock()
				jsonLine = line
				mu.Unlock()
				continue
			}
			if isErr {
				mu.Lock()
				errOut.WriteString(line + "\n")
				mu.Unlock()
			}
			if onProgress != nil {
				onProgress(line)
			}
		}
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); readPipe(stdout, false) }()
	go func() { defer readers.Done(); readPipe(stderr, true) }()

	// 必须先等两个读取协程读完再 Wait：Wait 会关闭管道，先 Wait 有可能
	// 丢掉最后一行 JSON，表现为偶发的"依赖分析未产生结果"。
	readers.Wait()
	err = cmd.Wait()
	if err != nil {
		mu.Lock()
		final := jsonLine
		mu.Unlock()
		if final == "" {
			// 分析脚本崩溃且无结果：用中文诊断（原因+建议+最近输出原文）替代干巴巴的退出码
			return nil, fmt.Errorf("依赖分析失败: %v\n%s", err, BuildAnalyzeDiagnosis(errOut.String()))
		}
		// 有 JSON 输出但进程退出码非 0（如 PyInstaller 缓存写警告），仍尝试解析
		var res config.DepsResult
		if e := json.Unmarshal([]byte(final), &res); e == nil {
			CleanPyiCache()
			return &res, nil
		}
		return nil, fmt.Errorf("依赖分析失败: %v", err)
	}

	mu.Lock()
	final := jsonLine
	mu.Unlock()
	if final == "" {
		return nil, fmt.Errorf("依赖分析未产生结果\n%s", BuildAnalyzeDiagnosis(errOut.String()))
	}
	var res config.DepsResult
	if e := json.Unmarshal([]byte(final), &res); e != nil {
		return nil, fmt.Errorf("依赖分析结果解析失败: %v", e)
	}
	CleanPyiCache()
	return &res, nil
}
