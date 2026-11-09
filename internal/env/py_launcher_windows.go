//go:build windows

package env

import (
	"regexp"
	"strings"

	"pypacker/internal/cmdutil"
)

// pyLauncherPathRe 从 `py -0p` 的一行里提取 python.exe 路径。
// 输出形如 " -V:3.12 *        C:\Users\me\AppData\Local\Programs\Python\Python312\python.exe"
// 或（非默认版本）" -V:3.11          C:\Python311\python.exe"。
// 注意：路径可能含空格（如 C:\Program Files\Python312\python.exe），
// 不能按空格切分取最后一段，必须从盘符开始整体匹配到行尾。
var pyLauncherPathRe = regexp.MustCompile(`(?i)([A-Za-z]:\\.*?python\.exe)\s*$`)

// pyLauncherEnvs 通过 Windows Python Launcher 的 `py -0p` 列出所有已安装 Python
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
		m := pyLauncherPathRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}
