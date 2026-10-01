package deps

import (
	"bufio"
	"fmt"
	"strings"

	"pypacker/internal/cmdutil"
)

// Install 将缺失依赖安装到用户所选 Python 环境中
// mirror 非空时以 pip install -i <mirror> 指定镜像源（如清华 / 阿里源）
// onLog 用于实时回传 pip 输出给前端；onProgress 用于回传安装进度百分比（0-100）与阶段说明
func Install(pythonPath string, pkgs []string, mirror string, onLog func(string), onProgress func(int, string), cancel <-chan struct{}) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := []string{"-m", "pip", "install", "--disable-pip-version-check"}
	if mirror != "" {
		args = append(args, "-i", mirror)
	}
	args = append(args, pkgs...)
	cmd := cmdutil.Command(pythonPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建输出管道失败: %v", err)
	}
	cmd.Stderr = cmd.Stdout // 合并 stderr 到 stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 pip 失败: %v", err)
	}

	// 输出转发结束信号（用于取消监听）
	done := make(chan struct{})
	// 监听取消请求：收到后强制结束 pip 进程
	if cancel != nil {
		go func() {
			select {
			case <-cancel:
				_ = cmd.Process.Kill()
			case <-done:
			}
		}()
	}

	var outBuf strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	// 简单进度：按 30%-90% 区间推进，每解析到一条下载/已缓存记录前进一步
	const (
		progressStart = 30
		progressEnd   = 90
	)
	var tick, total int
	for scanner.Scan() {
		line := scanner.Text()
		outBuf.WriteString(line + "\n")
		if onLog != nil {
			onLog(line)
		}
		// 统计需要下载/安装的包数量（Collecting 行）
		if strings.HasPrefix(line, "Collecting ") && !strings.Contains(line, " (from ") {
			total++
		}
		// 每完成一个包的下载/使用缓存，进度前进一档
		if strings.HasPrefix(line, "Downloading ") || strings.HasPrefix(line, "Using cached ") || strings.Contains(line, "Installing collected packages:") {
			tick++
			if onProgress != nil && total > 0 {
				p := progressStart + (progressEnd-progressStart)*tick/(total+1)
				if p > progressEnd {
					p = progressEnd
				}
				onProgress(p, fmt.Sprintf("正在安装依赖（%d/%d）", minInt(tick, total), total))
			}
		}
		// 安装成功结束行
		if strings.HasPrefix(line, "Successfully installed") {
			if onProgress != nil {
				onProgress(100, "依赖安装完成")
			}
		}
	}
	close(done)

	if err := cmd.Wait(); err != nil {
		// 用户主动取消：给出明确提示，不再附加诊断
		if cancel != nil {
			select {
			case <-cancel:
				if onLog != nil {
					onLog("安装已取消")
				}
				if onProgress != nil {
					onProgress(100, "安装已取消")
				}
				return fmt.Errorf("安装已取消")
			default:
			}
		}
		// 失败时附加中文诊断（原因 + 建议 + 最近日志），追加到日志并作为错误返回
		diag := BuildPipDiagnosis(outBuf.String())
		if onLog != nil {
			onLog("")
			onLog(diag)
		}
		if onProgress != nil {
			onProgress(100, "依赖安装失败")
		}
		return fmt.Errorf("%s", diag)
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
