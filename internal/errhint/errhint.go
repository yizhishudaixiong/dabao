// Package errhint 提供统一的"错误 → 中文原因 + 处理建议"诊断框架。
// 打包（PyInstaller）与依赖安装（pip）两类错误共用同一套匹配与拼装逻辑，
// 各自只需维护一张规则表，保证提示风格、展示位置完全一致。
package errhint

import (
	"regexp"
	"strings"
)

// Hint 中文诊断结果：原因 + 建议
type Hint struct {
	Reason string // 中文原因说明
	Advice string // 中文建议方案
}

// Rule 一条错误识别规则：输出中出现任一关键词即命中。
// Regex 可选：用于精确提取（如模块名），捕获组 1 会替换 Reason 中的 {1} 占位符。
// 带 Regex 的规则优先按正则匹配，正则未命中则继续用 Keywords 判断。
type Rule struct {
	Keywords []string       // 输出（小写化后）中出现任一子串即命中
	Regex    *regexp.Regexp // 可选：精确提取规则
	Reason   string         // 中文原因（可含 {1} 占位）
	Advice   string         // 中文建议
}

// Match 按规则表依次匹配输出文本，返回第一条命中的中文原因+建议；
// 全部未命中时返回 fallback 兜底提示。
func Match(out string, rules []Rule, fallback Hint) Hint {
	for _, r := range rules {
		if r.Regex != nil {
			if m := r.Regex.FindStringSubmatch(out); m != nil {
				reason := r.Reason
				if len(m) > 1 && m[1] != "" {
					reason = strings.ReplaceAll(reason, "{1}", m[1])
				}
				return Hint{Reason: reason, Advice: r.Advice}
			}
			continue
		}
		if ContainsAny(out, r.Keywords...) {
			return Hint{Reason: r.Reason, Advice: r.Advice}
		}
	}
	return fallback
}

// BuildDiagnosis 拼装完整诊断文本：
// 追加到日志最底部，同时作为最终错误消息返回给前端展示。
func BuildDiagnosis(title string, h Hint, out string, tailN int) string {
	var b strings.Builder
	b.WriteString("──────── " + title + " ────────\n")
	b.WriteString("【原因】" + h.Reason + "\n")
	b.WriteString("【建议】" + h.Advice + "\n")
	b.WriteString("──────── 最近输出 ────────\n")
	b.WriteString(Tail(out, tailN))
	return b.String()
}

// Tail 取输出末尾 n 行（报错信息通常在最后）
func Tail(s string, n int) string {
	trimmed := strings.TrimRight(s, "\n")
	if trimmed == "" {
		return "(无输出)"
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ContainsAny 判断字符串是否包含任意一个子串
func ContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
