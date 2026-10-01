//go:build !windows

package cmdutil

import (
	"os"
	"os/exec"
	"strings"
)

// Command 非 Windows 平台无需隐藏窗口；同样强制 Python 子进程 UTF-8 输出，避免中文乱码
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
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
