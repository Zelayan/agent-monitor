package task

import (
	"regexp"
)

// RedactedSecret 是敏感信息与密钥脱敏后的标准占位符。
const RedactedSecret = "[REDACTED_SECRET]"

var (
	// regexAPIKeyAssignments 匹配形如 password=..., db_password=..., api_key: "...", token: '...' 等键值对
	regexAPIKeyAssignments = regexp.MustCompile(`(?i)\b([a-zA-Z0-9_-]*(?:password|secret|token|api[_-]?key)\s*[:=]\s*["']?)[^"'\s\r\n]+(["']?)`)

	// regexOpenAIAnthropic 匹配 OpenAI / Anthropic / 通用 sk- 前缀密钥 (>=20 字符)
	regexOpenAIAnthropic = regexp.MustCompile(`\bsk-[A-Za-z0-9\-_]{20,}\b`)

	// regexGitHub 匹配 GitHub Personal Access Tokens (ghp_, gho_, ghs_, ghr_, github_pat_)
	regexGitHub = regexp.MustCompile(`\b(?:ghp_[A-Za-z0-9]{36}|gh[osr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,})\b`)

	// regexSlack 匹配 Slack 机器人与用户 Token (xoxb-, xoxp-, xoxa-, xoxr-)
	regexSlack = regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}\b`)

	// regexBearer 匹配 HTTP Authorization Bearer Token
	regexBearer = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9\-_.]+\b`)

	// regexMacUserPath 规范化 macOS 本机用户绝对路径 /Users/<user>/
	regexMacUserPath = regexp.MustCompile(`(/Users/)[a-zA-Z0-9._-]+(/)`)

	// regexLinuxUserPath 规范化 Linux 本机用户绝对路径 /home/<user>/
	regexLinuxUserPath = regexp.MustCompile(`(/home/)[a-zA-Z0-9._-]+(/)`)
)

// SanitizeString 对输入字符串执行敏感凭证、API 密钥及本地路径的脱敏清洗。
func SanitizeString(input string) string {
	if input == "" {
		return ""
	}

	// 1. 键值对敏感赋值 (password=..., token: ..., api_key=...)
	s := regexAPIKeyAssignments.ReplaceAllString(input, "${1}"+RedactedSecret+"${2}")

	// 2. OpenAI / Anthropic sk-... 密钥
	s = regexOpenAIAnthropic.ReplaceAllString(s, RedactedSecret)

	// 3. GitHub Token (ghp_..., github_pat_...)
	s = regexGitHub.ReplaceAllString(s, RedactedSecret)

	// 4. Slack Token (xoxb-..., xoxp-...)
	s = regexSlack.ReplaceAllString(s, RedactedSecret)

	// 5. Bearer Token
	s = regexBearer.ReplaceAllString(s, "${1}"+RedactedSecret)

	// 6. 本机绝对用户路径规范化
	s = regexMacUserPath.ReplaceAllString(s, "${1}***USER***${2}")
	s = regexLinuxUserPath.ReplaceAllString(s, "${1}***USER***${2}")

	return s
}

// Sanitize 返回对 Task 聚合根进行脱敏后的深拷贝副本，绝不篡改内存中的原始对象。
func (t *Task) Sanitize() *Task {
	if t == nil {
		return nil
	}

	cp := t.Clone()

	cp.RootGoal = SanitizeString(cp.RootGoal)
	cp.GoalSummary = SanitizeString(cp.GoalSummary)
	cp.Title = SanitizeString(cp.Title)
	cp.Prompt = SanitizeString(cp.Prompt)
	cp.Detail = SanitizeString(cp.Detail)
	cp.AbortReason = SanitizeString(cp.AbortReason)

	if cp.Runs != nil {
		for i := range cp.Runs {
			run := &cp.Runs[i]
			run.Prompt = SanitizeString(run.Prompt)
			run.Title = SanitizeString(run.Title)
			run.AIResponse = SanitizeString(run.AIResponse)
			run.Detail = SanitizeString(run.Detail)

			if run.Timeline != nil {
				for j := range run.Timeline {
					run.Timeline[j].Desc = SanitizeString(run.Timeline[j].Desc)
				}
			}
			if run.TraceSpans != nil {
				for j := range run.TraceSpans {
					run.TraceSpans[j].Detail = SanitizeString(run.TraceSpans[j].Detail)
					run.TraceSpans[j].AnomalyMsg = SanitizeString(run.TraceSpans[j].AnomalyMsg)
				}
			}
		}
		cp.Turns = cp.Runs
	}

	if cp.Timeline != nil {
		for i := range cp.Timeline {
			cp.Timeline[i].Desc = SanitizeString(cp.Timeline[i].Desc)
		}
	}

	if cp.TraceSpans != nil {
		for i := range cp.TraceSpans {
			cp.TraceSpans[i].Detail = SanitizeString(cp.TraceSpans[i].Detail)
			cp.TraceSpans[i].AnomalyMsg = SanitizeString(cp.TraceSpans[i].AnomalyMsg)
		}
	}

	if cp.ActiveSpans != nil {
		sanitizedSpans := make(map[string]TraceSpan, len(cp.ActiveSpans))
		for k, v := range cp.ActiveSpans {
			v.Detail = SanitizeString(v.Detail)
			v.AnomalyMsg = SanitizeString(v.AnomalyMsg)
			sanitizedSpans[k] = v
		}
		cp.ActiveSpans = sanitizedSpans
	}

	return cp
}
