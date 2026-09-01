package task

import (
	"fmt"
	"strings"
)

// FormatDuration 将秒数格式化为 "分m 秒s"。
func FormatDuration(sec int64) string {
	if sec < 0 {
		sec = 0
	}
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02dm %02ds", m, s)
}

// IsPlaceholderTitle 判断标题是否为可被真实 Prompt 覆盖的占位文案。
func IsPlaceholderTitle(title string) bool {
	t := strings.TrimSpace(title)
	if t == "" || t == "未命名" || t == "未命名任务" || t == "untitled session" {
		return true
	}
	if strings.HasPrefix(t, "CLI Task") {
		return true
	}
	if strings.HasSuffix(t, " 任务") {
		return true
	}
	return false
}

// IsRealTitle 判断标题是否为有效非占位标题。
func IsRealTitle(title string) bool {
	return strings.TrimSpace(title) != "" && !IsPlaceholderTitle(title)
}

// PlaceholderTitle 为指定 agent 生成兜底占位标题。
func PlaceholderTitle(agent string) string {
	if strings.TrimSpace(agent) == "" {
		return "AI Agent 任务"
	}
	return agent + " 任务"
}

// CleanPromptTitle 从 Prompt 中提取首行作为简洁标题，过滤常用前缀标记并限制长度。
func CleanPromptTitle(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 循环移除常见标记前缀
		prefixes := []string{"#task", "#Task", "[board]", "[Board]", "任务:", "任务：", "TODO:", "todo:"}
		changed := true
		for changed {
			changed = false
			line = strings.TrimSpace(line)
			for _, p := range prefixes {
				if strings.HasPrefix(line, p) {
					line = strings.TrimPrefix(line, p)
					changed = true
				}
			}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 80 {
			return line[:80]
		}
		return line
	}
	return ""
}
