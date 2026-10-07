package builder

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/paths"
)

// Workspace 表示一次打包任务的临时工作区，负责生命周期管理
type Workspace struct {
	Root string
}

// NewWorkspace 在系统临时目录下创建一个独立工作区（所有中间产物都在这里，便于整体清理）
func NewWorkspace() (*Workspace, error) {
	root := filepath.Join(paths.TempRoot(), fmt.Sprintf("job_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("创建临时工作区失败: %v", err)
	}
	return &Workspace{Root: root}, nil
}

// Run 在所选 Python 环境下执行 PyInstaller 打包，实时回调日志
// cancel 非 nil 时用于请求取消
// 若配置了代码加密（basic/deep），会先用 PyArmor 加密整个用户项目，再打包加密后的产物
func (w *Workspace) Run(cfg *config.BuildConfig, onLog func(string), cancel <-chan struct{}) error {
	// 实际入口脚本：默认用用户脚本；启用加密时替换为 PyArmor 加密后的脚本
	entry := cfg.EntryScript
	var extraData []string
	encProjRoot := ""

	if cfg.EncryptMode == "basic" || cfg.EncryptMode == "deep" {
		encEntry, runtimes, encRoot, err := w.encryptProject(cfg, onLog, cancel)
		if err != nil {
			return err
		}
		entry = encEntry
		encProjRoot = encRoot
		for _, rt := range runtimes {
			// Windows 打包使用分号分隔；runtime 目录原样放入 bundle 根目录
			extraData = append(extraData, rt+";"+filepath.Base(rt))
		}
		if onLog != nil {
			onLog("[加密] 已加密整个用户项目，开始打包...")
		}
	}

	args, err := BuildArgs(cfg, w.Root, entry, extraData, encProjRoot, onLog)
	if err != nil {
		return err
	}
	if onLog != nil {
		onLog("> " + cfg.PythonPath + " " + joinArgs(args))
	}

	cmd := cmdutil.Command(cfg.PythonPath, args...)
	cmd.Dir = w.Root

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建输出管道失败: %v", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 PyInstaller 失败: %v", err)
	}

	// 输出转发 + 取消监听
	done := make(chan struct{})
	var outBuf bytes.Buffer
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			outBuf.WriteString(line + "\n")
			if onLog != nil {
				onLog(line)
			}
		}
	}()

	if cancel != nil {
		go func() {
			select {
			case <-cancel:
				_ = cmd.Process.Kill()
			case <-done:
			}
		}()
	}

	err = cmd.Wait()
	<-done

	if err != nil {
		// Windows 下杀进程可能报 "signal: killed"，视为取消而非失败
		if cancel != nil {
			select {
			case <-cancel:
				return fmt.Errorf("打包已取消")
			default:
			}
		}
		// 分析 PyInstaller 完整输出，生成中文诊断（原因 + 建议 + 最近报错），
		// 追加到构建日志最底部，并作为最终错误消息返回
		out := outBuf.String()
		hint := ExplainBuildError(out)
		diag := BuildDiagnosisText(hint, out)
		if onLog != nil {
			onLog("")
			onLog(diag)
		}
		return fmt.Errorf("%s", diag)
	}

	// 打包成功：统计最终产物大小并汇报（UPX 由 PyInstaller 内部以 --lzma 极限压缩完成，
	// 拿不到压缩前大小，因此报告最终体积 + UPX 状态，让用户直观看到产物规模）
	if onLog != nil {
		onLog("")
		onLog(buildArtifactReport(cfg, w.Root))
	}

	// 「打包缓存加速」关闭时：打包完成后删除 PyInstaller 全局编译缓存（bincache），
	// 保证下次打包也不受旧缓存影响。只删 %LOCALAPPDATA%\pyinstaller，绝不触碰用户 Python 环境 / 工具自身分析缓存。
	if !cfg.CacheAccel {
		if onLog != nil {
			onLog("正在清理 PyInstaller 缓存…")
		}
		if err := CleanPyiGlobalCache(); err != nil {
			if onLog != nil {
				onLog("缓存清理失败：" + err.Error())
			}
		} else if onLog != nil {
			onLog("PyInstaller 缓存已清理（下次打包将全新构建）")
		}
	}
	return nil
}

// CleanPyiGlobalCache 删除 PyInstaller 全局缓存目录（Windows 下为 %LOCALAPPDATA%\pyinstaller）。
// 安全护栏：仅删除 PyInstaller 自身缓存目录，不涉及用户 Python 环境 / site-packages。
func CleanPyiGlobalCache() error {
	dir := filepath.Join(paths.LocalAppData(), "pyinstaller")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(dir)
}

// buildArtifactReport 统计构建产物（dist）的总大小并生成中文汇报文本。
// 单文件 -> 统计 <name>.exe；多文件 -> 统计整个 <name> 目录。
func buildArtifactReport(cfg *config.BuildConfig, workspace string) string {
	dist := filepath.Join(workspace, "dist")
	var size int64
	if cfg.OneFile {
		p := filepath.Join(dist, cfg.Name+".exe")
		if st, err := os.Stat(p); err == nil {
			size = st.Size()
		}
	} else {
		dir := filepath.Join(dist, cfg.Name)
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if st, err2 := d.Info(); err2 == nil {
					size += st.Size()
				}
			}
			return nil
		})
	}
	upxNote := "未启用 UPX 压缩"
	if cfg.UseUPX {
		upxNote = "已启用 UPX 压缩（LZMA 极限，体积更小，注意杀软误报风险）"
	}
	if size == 0 {
		return fmt.Sprintf("[完成] 打包成功，但未能统计到产物大小（%s）", upxNote)
	}
	return fmt.Sprintf("[完成] 打包成功！产物总大小：%s（%s）", formatSize(size), upxNote)
}

// formatSize 将字节数格式化为人类可读的大小（B / KB / MB / GB）
func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}


// CollectArtifact 将最终产物从工作区收集到用户指定输出目录。
// 单文件 -> 拷贝 dist/<name>.exe；多文件 -> 拷贝整个 dist/<name> 目录。
// 返回最终产物的绝对路径列表。
func (w *Workspace) CollectArtifact(cfg *config.BuildConfig, outputDir string) ([]string, error) {
	dist := filepath.Join(w.Root, "dist")
	if cfg.OneFile {
		src := filepath.Join(dist, cfg.Name+".exe")
		dst := filepath.Join(outputDir, cfg.Name+".exe")
		if err := copyFile(src, dst); err != nil {
			return nil, fmt.Errorf("收集单文件产物失败: %v", err)
		}
		return []string{dst}, nil
	}
	// 多文件：整个 <name> 目录是最终产物
	srcDir := filepath.Join(dist, cfg.Name)
	dstDir := filepath.Join(outputDir, cfg.Name)
	if err := copyDir(srcDir, dstDir); err != nil {
		return nil, fmt.Errorf("收集多文件产物失败: %v", err)
	}
	return []string{dstDir}, nil
}

// Cleanup 删除临时工作区（清理中间产物，绝不触碰用户 Python 环境）。
// 安全护栏：仅允许删除由本工具创建、位于系统临时目录 PythonPackTool 前缀下的工作区，
// 绝不可能触及用户 Python 环境 / site-packages。
func (w *Workspace) Cleanup() error {
	if w == nil || w.Root == "" {
		return nil
	}
	tempBase := paths.TempRoot()
	clean := filepath.Clean(w.Root)
	base := filepath.Clean(tempBase)
	if !isWithin(clean, base) {
		return fmt.Errorf("安全校验失败：拒绝删除非受管路径 %s", w.Root)
	}
	return os.RemoveAll(clean)
}

// isWithin 判断 child 是否位于 parent 之内（路径前缀 + 分隔符边界）
func isWithin(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func joinArgs(args []string) string {
	out := ""
	for _, a := range args {
		if out != "" {
			out += " "
		}
		if len(a) > 0 && (a[0] == '-' || containsSpace(a)) {
			out += "\"" + a + "\""
		} else {
			out += a
		}
	}
	return out
}

func containsSpace(s string) bool {
	for _, c := range s {
		if c == ' ' {
			return true
		}
	}
	return false
}
