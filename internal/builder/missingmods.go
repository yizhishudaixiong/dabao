package builder

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"pypacker/internal/config"
	"pypacker/internal/fsx"
)

// 构建产物里缺模块时，PyInstaller 会把它们写进 build/<name>/warn-<name>.txt：
//
//	missing module named requests - imported by C:\proj\main.py (top-level)
//	missing module named pwd - imported by shutil (delayed, optional), ...
//
// 全量展示没有意义：绝大多数是平台条件模块（pwd/grp/fcntl/posix）和第三方库的
// 可选导入，PyInstaller 自己的文件头也这么写。这里只挑「被用户自己的代码引用、
// 且不是 optional 导入」的项——那些才是运行时真会 ModuleNotFoundError 的。
//
// 这类问题的后果是产物一启动就崩，而构建本身是"成功"的，用户很容易漏掉，
// 所以在构建日志末尾单独提示一次。

var warnLineRe = regexp.MustCompile(`^missing module named (\S+) - imported by (.+)$`)

// missingModule 一条"产物缺少的模块"记录
type missingModule struct {
	Name      string   // 缺失的模块名（可能是 a.b 形式）
	Importers []string // 引用它的用户代码（相对项目目录，或模块名）
}

// missingModulesReport 扫描工作区里的 warn-*.txt，返回面向用户的提示文本。
// 没有命中（或没有 warn 文件）时返回空串，调用方直接跳过。
func missingModulesReport(workspace string, cfg *config.BuildConfig) string {
	if cfg == nil || cfg.EntryScript == "" {
		return ""
	}
	projDir := filepath.Dir(cfg.EntryScript)
	local := localModuleNames(projDir)
	if len(local) == 0 {
		return ""
	}

	warnFiles, _ := filepath.Glob(filepath.Join(workspace, "build", "*", "warn-*.txt"))
	if len(warnFiles) == 0 {
		return ""
	}

	found := map[string]map[string]bool{} // 模块名 -> 引用它的本地文件集合
	for _, f := range warnFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			m := warnLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name, importers := m[1], m[2]
			if isStdlibLooking(name) {
				continue
			}
			for _, item := range splitImporters(importers) {
				who, kinds := parseImporter(item)
				if who == "" || strings.Contains(kinds, "optional") {
					continue // try/except 里的可选导入，缺了程序会自己降级
				}
				display, ok := localImporter(who, projDir, local)
				if !ok {
					continue // 标准库/第三方库内部的引用，不是用户的问题
				}
				if found[name] == nil {
					found[name] = map[string]bool{}
				}
				found[name][display] = true
			}
		}
	}
	if len(found) == 0 {
		return ""
	}

	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("[依赖检查] 产物里缺少以下模块，运行到对应代码时会报 ModuleNotFoundError：\n")
	for _, n := range names {
		srcs := make([]string, 0, len(found[n]))
		for s := range found[n] {
			srcs = append(srcs, s)
		}
		sort.Strings(srcs)
		b.WriteString("  - " + n + "  ← " + strings.Join(srcs, "、") + "\n")
	}
	b.WriteString("处理：到「依赖检查」页安装对应依赖后重新打包；若确认是可选功能，可忽略本条提示。")
	return b.String()
}

// splitImporters 把 "a (top-level), b (delayed, optional)" 拆成单个条目。
// 不能用 strings.Split(s, ", ")：括号里的修饰词本身就用逗号分隔。
func splitImporters(s string) []string {
	parts := strings.Split(s, "), ")
	for i := 0; i < len(parts)-1; i++ {
		parts[i] += ")"
	}
	return parts
}

// parseImporter 解析单个条目："C:\proj\main.py (top-level)" -> 路径 + "top-level"
func parseImporter(item string) (who, kinds string) {
	item = strings.TrimSpace(item)
	i := strings.LastIndex(item, " (")
	if i < 0 || !strings.HasSuffix(item, ")") {
		return strings.TrimSpace(item), ""
	}
	return strings.TrimSpace(item[:i]), item[i+2 : len(item)-1]
}

// localModuleNames 项目目录下所有 .py 的模块名（小写），用于判断某个 import 是不是用户自己的代码
func localModuleNames(projDir string) map[string]bool {
	files, err := collectProjectPyFiles(projDir)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(files))
	for _, rel := range files {
		base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		if base != "" && base != "__init__" {
			out[strings.ToLower(base)] = true
		}
	}
	return out
}

// localImporter 判断 warn 文件里记录的"导入方"是否是用户自己的代码。
// PyInstaller 对入口脚本写完整路径，对其它模块只写模块名。
func localImporter(who, projDir string, local map[string]bool) (string, bool) {
	// ① 完整路径：位于项目目录内就是用户代码
	if filepath.IsAbs(who) || strings.ContainsAny(who, `/\`) {
		if !fsx.IsWithin(who, projDir) {
			return "", false
		}
		if rel, err := filepath.Rel(projDir, who); err == nil {
			return filepath.ToSlash(rel), true
		}
		return filepath.Base(who), true
	}
	// ② 模块名：取顶层段，命中项目里的 .py 即视为用户代码
	top := strings.ToLower(strings.SplitN(who, ".", 2)[0])
	if local[top] {
		return who, true
	}
	return "", false
}

// isStdlibLooking 粗筛掉明显的标准库模块，避免把平台条件模块报给用户。
// 只用来减少噪音，判错也只是少一条提示，不影响正确性。
var stdlibLooking = map[string]bool{
	"pwd": true, "grp": true, "fcntl": true, "posix": true, "resource": true,
	"termios": true, "tty": true, "pty": true, "readline": true, "crypt": true,
	"_posixsubprocess": true, "_posixshmem": true, "_scproxy": true, "_frozen_importlib": true,
	"_frozen_importlib_external": true, "posixpath": true, "ntpath": true,
	"java": true, "org": true, "vms_lib": true, "msvcrt": true, "winreg": true,
}

func isStdlibLooking(name string) bool {
	top := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	return stdlibLooking[top]
}
