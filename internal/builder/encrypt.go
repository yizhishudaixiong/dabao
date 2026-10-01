package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pypacker/internal/cmdutil"
	"pypacker/internal/config"
	"pypacker/internal/deps"
)

// encryptProject 使用 PyArmor 加密用户项目中的全部自写 .py 模块（递归，逐文件加密），
// 返回加密后的入口脚本路径、需要随包分发的 pyarmor_runtime 运行时目录、
// 加密项目根目录（PyInstaller 需从这里收集加密模块）。
//
// 加密产物全部生成在临时工作区内，打包完成后随工作区一起整体清理，
// 不会在用户磁盘上留下任何中间文件，也不触碰用户 Python 环境。
//
// 两档加密（均使用 PyArmor 免费版功能）：
//   - basic 基础加密：字节码高强度加密 + 代码对象保护，可防绝大多数反编译工具
//   - deep  深度加密：在基础之上增加 --enable-jit（JIT 动态指令保护）
//
// 注意：不能用 --private / --assert-call / --assert-import 等 restrict 选项。
// 实测这些选项会与 PySide6/PyQt 的 shibokensupport（用 inspect 检查已加载模块源码）
// 冲突，打包后的程序运行时报 RuntimeError: unauthorized use of script，程序崩溃。
// 去掉 restrict 选项后 PySide6 项目加密打包运行正常。
//
// 试用版硬限制：单个代码对象编译后超过 32768 字节（32KB）的文件无法加密（out of license）。
// 处理策略（方案A）：
//   - 逐文件加密，超限文件自动跳过并以明文打包（界面/日志明确警告）
//   - 入口脚本超限则直接报错停止（入口不加密则加密无意义）
func (w *Workspace) encryptProject(cfg *config.BuildConfig, onLog func(string)) (string, []string, string, error) {
	if cfg.EntryScript == "" {
		return "", nil, "", fmt.Errorf("请先选择 Python 入口脚本")
	}
	if cfg.PythonPath == "" {
		return "", nil, "", fmt.Errorf("请先选择 Python 环境")
	}

	// 1. 定位所选 Python 环境中的 pyarmor 可执行文件（PyArmor 8+ 为独立 CLI）
	pyarmorExe := deps.FindPyarmor(cfg.PythonPath)
	if pyarmorExe == "" {
		return "", nil, "", fmt.Errorf("所选 Python 环境未安装 PyArmor，请回到「选择环境」页一键安装")
	}
	ok, ver := deps.CheckPyarmor(cfg.PythonPath)
	if !ok {
		return "", nil, "", fmt.Errorf("所选 Python 环境未安装 PyArmor，请回到「选择环境」页一键安装")
	}
	if onLog != nil {
		onLog("[加密] 检测到 PyArmor: " + ver)
	}

	projDir := filepath.Dir(cfg.EntryScript)
	entryBase := filepath.Base(cfg.EntryScript)

	// 2. 递归收集项目内所有自写 .py（相对 projDir 的路径），排除常见无关目录
	pyFiles, err := collectProjectPyFiles(projDir)
	if err != nil {
		return "", nil, "", fmt.Errorf("扫描项目代码失败: %v", err)
	}
	if len(pyFiles) == 0 {
		return "", nil, "", fmt.Errorf("项目目录中没有找到 .py 文件: %s", projDir)
	}

	// 3. 加密项目根目录（模拟 pyarmor gen -r 的结构：enc/<项目名>/xxx.py）
	encOut := filepath.Join(w.Root, "encrypted")
	encProjRoot := filepath.Join(encOut, filepath.Base(projDir))
	if err := os.MkdirAll(encProjRoot, 0o755); err != nil {
		return "", nil, "", fmt.Errorf("创建加密目录失败: %v", err)
	}

	deepArgs := []string{"--enable-jit"}

	// 4. 第一轮：逐文件加密（完整参数）
	//    encEntry: 加密后的入口脚本（encProjRoot 下）
	//    runtimes:  pyarmor_runtime 运行时目录（内容相同，取第一个成功输出的）
	//    skipped:   超限未加密的文件（明文复制进加密根，保证 PyInstaller 能收集到）
	var encEntry string
	var runtimes []string
	var skipped []string
	for i, rel := range pyFiles {
		abs := filepath.Join(projDir, rel)
		outDir := filepath.Join(w.Root, "enc_tmp", fmt.Sprintf("m%d", i))
		args := []string{"gen", "-O", outDir}
		if cfg.EncryptMode == "deep" {
			args = append(args, deepArgs...)
		}
		args = append(args, abs)

		if onLog != nil {
			onLog(fmt.Sprintf("[加密] 正在加密 %s ...", rel))
		}
		cmd := cmdutil.Command(pyarmorExe, args...)
		cmd.Dir = w.Root
		_, err := cmd.CombinedOutput()
		if err != nil {
			// 超限（试用版限制）：跳过加密，明文兜底
			if onLog != nil {
				onLog(fmt.Sprintf("[加密] ⚠ %s 超过 PyArmor 试用版限制（单个代码对象 32KB），将以明文打包", rel))
			}
			// 入口脚本必须加密，否则加密无意义
			if rel == entryBase {
				return "", nil, "", fmt.Errorf("入口脚本 %s 超过 PyArmor 试用版限制（单个代码对象 32KB），无法加密。请缩小入口脚本，或购买 PyArmor 基础版解锁大脚本加密", entryBase)
			}
			skipped = append(skipped, rel)
			// 明文复制到加密根对应位置，保证 PyInstaller 从加密根也能收集到它
			if err := copyInto(projDir, rel, encProjRoot); err != nil {
				return "", nil, "", fmt.Errorf("复制明文模块失败 %s: %v", rel, err)
			}
			continue
		}
		// 加密成功：把加密后的 .py 复制到加密根对应相对位置（保持项目目录结构）
		encFile := filepath.Join(outDir, filepath.Base(rel))
		if !fileExists(encFile) {
			return "", nil, "", fmt.Errorf("未找到加密产物 %s: %v", rel, err)
		}
		dst := filepath.Join(encProjRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", nil, "", fmt.Errorf("创建加密目录失败: %v", err)
		}
		if err := copyFile(encFile, dst); err != nil {
			return "", nil, "", fmt.Errorf("复制加密产物失败 %s: %v", rel, err)
		}
		if rel == entryBase {
			encEntry = dst
		}
		// 收集 runtime（所有文件输出的 runtime 内容一致，取第一个）
		if len(runtimes) == 0 {
			if entries, err := os.ReadDir(outDir); err == nil {
				for _, e := range entries {
					if e.IsDir() && strings.HasPrefix(e.Name(), "pyarmor_runtime") {
						runtimes = append(runtimes, filepath.Join(outDir, e.Name()))
					}
				}
			}
		}
	}
	if encEntry == "" {
		return "", nil, "", fmt.Errorf("未找到加密后的入口脚本: %s", entryBase)
	}

	// 6. 校验运行时目录存在
	if len(runtimes) == 0 {
		return "", nil, "", fmt.Errorf("未找到 PyArmor 运行时目录，加密可能未成功")
	}

	// 7. 汇总未加密文件警告
	if len(skipped) > 0 && onLog != nil {
		onLog(fmt.Sprintf("[加密] 以下 %d 个文件超过试用版限制，未加密（明文打包）：%s", len(skipped), strings.Join(skipped, "、")))
		onLog("[加密] 如需加密大文件，请购买 PyArmor 基础版（解锁 32KB 限制），或将大文件拆分为多个小文件")
	}

	return encEntry, runtimes, encProjRoot, nil
}

// collectProjectPyFiles 递归收集项目目录下所有 .py 文件（相对路径），
// 跳过 __pycache__、venv、.git、build、dist、site-packages 等无关目录。
func collectProjectPyFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "__pycache__" || name == "venv" || name == ".venv" || name == ".git" ||
				name == "build" || name == "dist" || name == "site-packages" || strings.HasPrefix(name, "env")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".py") {
			rel, err := filepath.Rel(root, path)
			if err == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	return files, err
}

// copyInto 把源目录下 rel 相对路径的文件复制到目标目录对应位置（自动建子目录）
func copyInto(srcRoot, rel, dstRoot string) error {
	src := filepath.Join(srcRoot, rel)
	dst := filepath.Join(dstRoot, rel)
	return copyFile(src, dst)
}
