//go:build !windows

package env

// pyLauncherEnvs 非 Windows 平台无 Python Launcher，返回空
func pyLauncherEnvs() []string {
	return nil
}
