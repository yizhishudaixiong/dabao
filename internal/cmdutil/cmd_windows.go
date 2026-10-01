//go:build windows

package cmdutil

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Command 创建子进程命令。Windows 下设置隐藏窗口标志，
// 避免 GUI 程序启动 python / pip / PyInstaller 等控制台程序时闪烁黑框。
// 同时强制 Python 子进程以 UTF-8 输出（PYTHONIOENCODING），
// 否则 Windows 默认 GBK 编码会让日志里的中文（如中文路径）乱码。
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	// 在继承父进程环境的基础上追加 UTF-8 输出设置（不能直接覆盖 cmd.Env，
	// 否则会丢失 PATH 等环境变量导致命令无法运行）
	env := os.Environ()
	hasIO := false
	for _, e := range env {
		if strings.HasPrefix(e, "PYTHONIOENCODING=") {
			hasIO = true
			break
		}
	}
	if !hasIO {
		env = append(env, "PYTHONIOENCODING=utf-8")
	}
	cmd.Env = env
	return cmd
}
