package builder

import (
	"fmt"
	"sort"
	"strings"

	"pypacker/internal/config"
)

// 构建前的资源核对：代码里引用、磁盘上确实存在、但没被勾选进打包清单的文件。
//
// 这类文件不会进产物，程序跑到对应代码就报 FileNotFoundError —— 而构建本身是
// "成功"的，用户要等运行才发现。分析结果本来就有（「打包模式」页的资源分析），
// 这里只是把它挪到构建入口再核对一次，避免用户跳过那一步直接打包。
//
// 只提示、不阻断：有些文件确实是从外部目录读取的（运行期生成、用户自己放），
// 不应该打进包，也不该因此拦住构建。

// resourceGap 一个"没进包"的资源及其引用位置
type resourceGap struct {
	Ref  string   // 代码里的写法（相对路径，便于用户对照）
	Srcs []string // 引用位置 "文件:行号"
}

// unbundledResources 从资源分析结果里挑出「存在但未勾选」的项，按磁盘路径去重。
// 同一个文件被多处引用时，把来源合并成一条，避免刷屏。
func unbundledResources(deps []config.ResourceDep) []resourceGap {
	type acc struct {
		ref  string
		srcs []string
	}
	byPath := map[string]*acc{}
	for _, d := range deps {
		if !d.Exists || d.Included || d.DiskPath == "" {
			continue
		}
		key := strings.ToLower(d.DiskPath)
		a := byPath[key]
		if a == nil {
			ref := d.Ref
			if ref == "" {
				ref = d.DiskPath
			}
			a = &acc{ref: ref}
			byPath[key] = a
		}
		if d.SourceFile != "" {
			src := d.SourceFile
			if d.Line > 0 {
				src = fmt.Sprintf("%s:%d", d.SourceFile, d.Line)
			}
			a.srcs = append(a.srcs, src)
		}
	}

	gaps := make([]resourceGap, 0, len(byPath))
	for _, a := range byPath {
		sort.Strings(a.srcs)
		srcs := a.srcs
		if len(srcs) > 3 {
			srcs = append(srcs[:3:3], fmt.Sprintf("等 %d 处", len(a.srcs)))
		}
		gaps = append(gaps, resourceGap{Ref: a.ref, Srcs: srcs})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].Ref < gaps[j].Ref })
	return gaps
}

// formatResourceGap 生成构建日志里的提示文本；没有缺口时返回空串。
func formatResourceGap(gaps []resourceGap) string {
	if len(gaps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[资源检查] 有 %d 个代码引用的文件没有加入打包清单，产物运行时可能读不到：\n", len(gaps)))
	for _, g := range gaps {
		b.WriteString("  - " + g.Ref)
		if len(g.Srcs) > 0 {
			b.WriteString("  ← " + strings.Join(g.Srcs, "、"))
		}
		b.WriteString("\n")
	}
	b.WriteString("处理：到「打包模式」页勾选对应资源后重新打包；如果这些文件是程序运行时从外部目录读取的，可以忽略。")
	return b.String()
}

// checkUnbundledResources 执行一次资源分析并返回提示文本。
// 分析本身失败时静默返回空串——构建不该因为"检查不出来"而中断。
func checkUnbundledResources(cfg *config.BuildConfig) string {
	if cfg == nil || cfg.EntryScript == "" || cfg.PythonPath == "" {
		return ""
	}
	deps, err := AnalyzeResourceDeps(cfg.PythonPath, cfg.EntryScript, cfg.Resources)
	if err != nil || len(deps) == 0 {
		return ""
	}
	return formatResourceGap(unbundledResources(deps))
}
