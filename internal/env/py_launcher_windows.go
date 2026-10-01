//go:build windows

package env

import (
	"strings"

	"pypacker/internal/cmdutil"
)

// pyLauncherEnvs 通过 Windows Python Launcher 的 `py -0p` 列出所有已安装 Python
// 输出格式示例:  -V:3.12 * C:\Python312\python.exe
func pyLauncherEnvs() []string {
	var out []string
	cmd := cmdutil.Command("py", "-0p")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		path := fields[len(fields)-1]
		path = strings.Trim(path, "\"")
		if strings.HasSuffix(strings.ToLower(path), "python.exe") {
			out = append(out, path)
		}
	}
	return out
}
