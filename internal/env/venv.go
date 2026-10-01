package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
)

// CreateVenv 使用物理 Python 创建一个虚拟环境（Windows 下为 venv\Scripts\python.exe）。
// 说明：虚拟环境的 Python 版本 = 物理 Python 的版本，无法自行选择版本。
// 中文路径：Python 3.8+ 的 venv 支持中文路径，个别带底层扩展的包可能不兼容，
// 因此界面建议优先使用英文路径，但不强制。
// onLog 用于回传创建过程日志；onProgress 回传进度百分比与阶段说明。
func CreateVenv(basePython, venvPath string, onLog func(string), onProgress func(int, string)) (config.PythonEnv, error) {
	if basePython == "" || venvPath == "" {
		return config.PythonEnv{}, fmt.Errorf("物理 Python 路径和虚拟环境路径不能为空")
	}

	// 目标目录已存在且非空时拒绝创建，避免覆盖用户已有目录
	if fi, err := os.Stat(venvPath); err == nil {
		if !fi.IsDir() {
			return config.PythonEnv{}, fmt.Errorf("目标路径已存在且不是文件夹：%s", venvPath)
		}
		entries, _ := os.ReadDir(venvPath)
		if len(entries) > 0 {
			return config.PythonEnv{}, fmt.Errorf("目标文件夹已存在且不为空：%s\n为避免误删你的文件，请换一个不存在的路径", venvPath)
		}
	}

	if onProgress != nil {
		onProgress(5, "正在创建虚拟环境")
	}
	if onLog != nil {
		onLog("> " + basePython + " -m venv " + venvPath)
	}

	cmd := cmdutil.Command(basePython, "-m", "venv", venvPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		diag := strings.TrimSpace(string(out))
		if diag == "" {
			diag = "创建失败（exit status " + fmt.Sprint(err) + "）"
		}
		if onLog != nil {
			onLog(diag)
		}
		return config.PythonEnv{}, fmt.Errorf("虚拟环境创建失败：%s\n建议：检查物理 Python 是否正常可用（python -m venv 是否支持）", diag)
	}
	if onLog != nil {
		onLog(strings.TrimSpace(string(out)))
	}

	// 虚拟环境中的 python.exe（Windows）
	venvPython := filepath.Join(venvPath, "Scripts", "python.exe")
	if _, err := os.Stat(venvPython); err != nil {
		return config.PythonEnv{}, fmt.Errorf("虚拟环境创建不完整，未找到 %s\n建议：重新创建，或确认物理 Python 支持 venv", venvPython)
	}

	// 检测新环境信息（版本 / pip / PyInstaller / PyArmor）
	detected, err := Detect(venvPython)
	if err != nil {
		return config.PythonEnv{}, fmt.Errorf("虚拟环境创建成功，但检测环境信息失败: %v", err)
	}
	// Detect 不生成名称/虚拟环境标记，这里手动补全，否则界面下拉框显示为空
	detected.IsVirtual = true
	detected.Root = venvPath
	detected.Name = displayName(&detected)
	if onProgress != nil {
		onProgress(15, "虚拟环境创建完成")
	}
	return detected, nil
}
