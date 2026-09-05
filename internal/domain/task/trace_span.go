package task

import (
	"fmt"
	"strings"
)

const (
	// DefaultSpanAnomalyThresholdMs 默认判定工具执行卡顿/未闭合子进程的超时阈值（60秒）
	DefaultSpanAnomalyThresholdMs int64 = 60000

	// TraceSpan 状态常量
	SpanStatusRunning   = "running"
	SpanStatusCompleted = "completed"
	SpanStatusFailed    = "failed"

	// AnomalyMsgStuckTool 默认卡死/超时异常诊断提示
	AnomalyMsgStuckTool = "Tool execution exceeded threshold (possible stuck process or unclosed interactive sub-shell)"
)

// TraceSpan 记录单次工具调用或子动作的细粒度追踪跨度（值对象）。
type TraceSpan struct {
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId,omitempty"`
	ToolName     string `json:"toolName"`
	Detail       string `json:"detail,omitempty"`
	StartMs      int64  `json:"startMs"`
	EndMs        int64  `json:"endMs,omitempty"`
	DurationMs   int64  `json:"durationMs,omitempty"`
	Status       string `json:"status"` // "running", "completed", "failed"
	IsAnomaly    bool   `json:"isAnomaly,omitempty"`
	AnomalyMsg   string `json:"anomalyMsg,omitempty"`
}

// AnomalyInfo 封装检测出的卡顿/异常跨度诊断信息。
type AnomalyInfo struct {
	SpanID      string `json:"spanId"`
	ToolName    string `json:"toolName"`
	DurationMs  int64  `json:"durationMs"`
	ThresholdMs int64  `json:"thresholdMs"`
	AnomalyType string `json:"anomalyType"` // e.g. "stuck_tool"
	Message     string `json:"message"`
}

// IsToolStartHook 判定事件是否为工具调用的启动前置钩子。
func IsToolStartHook(event string) bool {
	switch event {
	case "beforeToolUse", "PreToolUse", "preToolUse",
		"beforeShellExecution", "beforeMCPExecution",
		"toolStart", "subagentStart", "SubagentStart":
		return true
	default:
		return false
	}
}

// IsToolEndHook 判定事件是否为工具正常完成的收尾后置钩子。
func IsToolEndHook(event string) bool {
	switch event {
	case "afterToolUse", "PostToolUse", "postToolUse",
		"afterShellExecution", "afterMCPExecution",
		"toolSuccess", "toolComplete", "subagentStop", "SubagentStop":
		return true
	default:
		return false
	}
}

// IsToolFailureHook 判定事件是否为工具执行失败的后置钩子。
func IsToolFailureHook(event string) bool {
	switch event {
	case "toolFailure", "PostToolUseFailure", "postToolUseFailure", "toolError":
		return true
	default:
		return false
	}
}

// ResolveToolName 从 payload、事件名及 detail 综合提取规范化的工具名称。
func ResolveToolName(event string, payloadToolName string, detail string) string {
	if trimmed := strings.TrimSpace(payloadToolName); trimmed != "" {
		return trimmed
	}
	switch event {
	case "beforeShellExecution", "afterShellExecution":
		return "Bash"
	case "beforeMCPExecution", "afterMCPExecution":
		return "MCP"
	case "subagentStart", "subagentStop", "SubagentStart", "SubagentStop":
		return "subagent"
	}
	if strings.Contains(detail, "执行命令:") || strings.Contains(detail, "运行终端命令:") {
		return "Bash"
	}
	if idx := strings.Index(detail, "调用工具: "); idx != -1 {
		tool := strings.TrimSpace(detail[idx+len("调用工具: "):])
		if endIdx := strings.IndexAny(tool, " \t\r\n`"); endIdx != -1 {
			tool = strings.Trim(tool[:endIdx], "`")
		}
		if tool != "" {
			return tool
		}
	}
	if idx := strings.Index(detail, "工具执行失败: "); idx != -1 {
		tool := strings.TrimSpace(detail[idx+len("工具执行失败: "):])
		if endIdx := strings.IndexAny(tool, " \t\r\n`"); endIdx != -1 {
			tool = strings.Trim(tool[:endIdx], "`")
		}
		if tool != "" {
			return tool
		}
	}
	if idx := strings.Index(detail, "工具执行失败 ["); idx != -1 {
		tool := detail[idx+len("工具执行失败 ["):]
		if endIdx := strings.Index(tool, "]"); endIdx != -1 {
			return strings.TrimSpace(tool[:endIdx])
		}
	}
	return "tool"
}

// StartTraceSpan 开启并记录一条新的工具执行跨度。
func (t *Task) StartTraceSpan(p EventPayload, startMs int64) *TraceSpan {
	if t.ActiveSpans == nil {
		t.ActiveSpans = make(map[string]TraceSpan)
	}
	toolName := ResolveToolName(p.Event, p.ToolName, p.Detail)
	spanID := p.SpanID
	if spanID == "" {
		spanID = fmt.Sprintf("span-%s-%d-%d", strings.ToLower(toolName), startMs, len(t.TraceSpans)+1)
	}

	span := TraceSpan{
		SpanID:       spanID,
		ParentSpanID: p.ParentSpanID,
		ToolName:     toolName,
		Detail:       p.Detail,
		StartMs:      startMs,
		Status:       SpanStatusRunning,
	}

	t.ActiveSpans[spanID] = span
	t.TraceSpans = append(t.TraceSpans, span)

	if len(t.Runs) > 0 {
		curRun := &t.Runs[len(t.Runs)-1]
		curRun.TraceSpans = append(curRun.TraceSpans, span)
	}

	return &span
}

// CompleteTraceSpan 完成一条活跃的工具跨度，计算其持续耗时并检测是否异常超时。
func (t *Task) CompleteTraceSpan(p EventPayload, endMs int64, isFailure bool) *TraceSpan {
	if len(t.ActiveSpans) == 0 {
		// 没有匹配的活跃跨度（例如 Pre 钩子丢失或乱序晚到），补记一条已完成/失败的跨度
		toolName := ResolveToolName(p.Event, p.ToolName, p.Detail)
		spanID := p.SpanID
		if spanID == "" {
			spanID = fmt.Sprintf("span-%s-%d-%d", strings.ToLower(toolName), endMs, len(t.TraceSpans)+1)
		}
		status := SpanStatusCompleted
		if isFailure {
			status = SpanStatusFailed
		}
		span := TraceSpan{
			SpanID:       spanID,
			ParentSpanID: p.ParentSpanID,
			ToolName:     toolName,
			Detail:       p.Detail,
			StartMs:      endMs,
			EndMs:        endMs,
			DurationMs:   0,
			Status:       status,
		}
		t.TraceSpans = append(t.TraceSpans, span)
		if len(t.Runs) > 0 {
			curRun := &t.Runs[len(t.Runs)-1]
			curRun.TraceSpans = append(curRun.TraceSpans, span)
		}
		return &span
	}

	// 匹配对应的活跃 span
	var matchedID string
	if p.SpanID != "" {
		if _, ok := t.ActiveSpans[p.SpanID]; ok {
			matchedID = p.SpanID
		}
	}

	targetTool := ResolveToolName(p.Event, p.ToolName, p.Detail)
	if matchedID == "" && targetTool != "" {
		// 按 ToolName 匹配最新（最近启动）的一个
		var latestStart int64 = -1
		for id, s := range t.ActiveSpans {
			if strings.EqualFold(s.ToolName, targetTool) {
				if s.StartMs >= latestStart {
					latestStart = s.StartMs
					matchedID = id
				}
			}
		}
	}

	if matchedID == "" {
		// 如果只有 1 个活跃跨度，直接匹配该跨度
		if len(t.ActiveSpans) == 1 {
			for id := range t.ActiveSpans {
				matchedID = id
				break
			}
		} else {
			// 匹配启动时间最晚的一个
			var latestStart int64 = -1
			for id, s := range t.ActiveSpans {
				if s.StartMs >= latestStart {
					latestStart = s.StartMs
					matchedID = id
				}
			}
		}
	}

	if matchedID == "" {
		return nil
	}

	span := t.ActiveSpans[matchedID]
	delete(t.ActiveSpans, matchedID)

	span.EndMs = endMs
	if span.EndMs < span.StartMs {
		span.EndMs = span.StartMs // 单调性保护
	}
	span.DurationMs = span.EndMs - span.StartMs
	if isFailure {
		span.Status = SpanStatusFailed
	} else {
		span.Status = SpanStatusCompleted
	}
	if p.Detail != "" && (span.Detail == "" || isFailure) {
		span.Detail = p.Detail
	}

	// 异常耗时检测
	if span.DurationMs > DefaultSpanAnomalyThresholdMs {
		span.IsAnomaly = true
		span.AnomalyMsg = AnomalyMsgStuckTool
	}

	// 更新 Task.TraceSpans 中的条目
	for i := range t.TraceSpans {
		if t.TraceSpans[i].SpanID == matchedID {
			t.TraceSpans[i] = span
			break
		}
	}

	// 更新各 Run 中的条目
	for r := range t.Runs {
		for s := range t.Runs[r].TraceSpans {
			if t.Runs[r].TraceSpans[s].SpanID == matchedID {
				t.Runs[r].TraceSpans[s] = span
				break
			}
		}
	}

	return &span
}

// CloseLingeringSpans 会话/轮次终态时，收口所有仍在 running 状态的活跃 span。
func (t *Task) CloseLingeringSpans(nowMs int64, isSessionFailed bool) {
	if len(t.ActiveSpans) == 0 {
		return
	}
	for id, span := range t.ActiveSpans {
		span.EndMs = nowMs
		if span.EndMs < span.StartMs {
			span.EndMs = span.StartMs
		}
		span.DurationMs = span.EndMs - span.StartMs
		if isSessionFailed {
			span.Status = SpanStatusFailed
		} else {
			span.Status = SpanStatusCompleted
		}
		if span.DurationMs > DefaultSpanAnomalyThresholdMs {
			span.IsAnomaly = true
			span.AnomalyMsg = AnomalyMsgStuckTool
		}
		for i := range t.TraceSpans {
			if t.TraceSpans[i].SpanID == id {
				t.TraceSpans[i] = span
				break
			}
		}
		for r := range t.Runs {
			for s := range t.Runs[r].TraceSpans {
				if t.Runs[r].TraceSpans[s].SpanID == id {
					t.Runs[r].TraceSpans[s] = span
					break
				}
			}
		}
		delete(t.ActiveSpans, id)
	}
}

// DetectAnomalies 基于默认超时阈值（60秒）检测卡死/超时的活跃跨度。
func (t *Task) DetectAnomalies(nowMs int64) []AnomalyInfo {
	return t.DetectAnomaliesWithThreshold(nowMs, DefaultSpanAnomalyThresholdMs)
}

// DetectAnomaliesWithThreshold 基于指定超时阈值毫秒数检测卡死/超时的活跃跨度。
func (t *Task) DetectAnomaliesWithThreshold(nowMs int64, thresholdMs int64) []AnomalyInfo {
	if thresholdMs <= 0 {
		thresholdMs = DefaultSpanAnomalyThresholdMs
	}
	var anomalies []AnomalyInfo
	for id, span := range t.ActiveSpans {
		duration := nowMs - span.StartMs
		if duration < 0 {
			duration = 0
		}
		if duration > thresholdMs {
			span.IsAnomaly = true
			span.AnomalyMsg = AnomalyMsgStuckTool
			span.DurationMs = duration
			t.ActiveSpans[id] = span

			for i := range t.TraceSpans {
				if t.TraceSpans[i].SpanID == id {
					t.TraceSpans[i].IsAnomaly = true
					t.TraceSpans[i].AnomalyMsg = span.AnomalyMsg
					t.TraceSpans[i].DurationMs = duration
					break
				}
			}
			for r := range t.Runs {
				for s := range t.Runs[r].TraceSpans {
					if t.Runs[r].TraceSpans[s].SpanID == id {
						t.Runs[r].TraceSpans[s].IsAnomaly = true
						t.Runs[r].TraceSpans[s].AnomalyMsg = span.AnomalyMsg
						t.Runs[r].TraceSpans[s].DurationMs = duration
						break
					}
				}
			}

			anomalies = append(anomalies, AnomalyInfo{
				SpanID:      id,
				ToolName:    span.ToolName,
				DurationMs:  duration,
				ThresholdMs: thresholdMs,
				AnomalyType: "stuck_tool",
				Message:     span.AnomalyMsg,
			})
		}
	}
	return anomalies
}

// GetActiveTraceSpans 返回当前仍在执行中的所有活跃追踪跨度，并动态填充其实时耗时。
func (t *Task) GetActiveTraceSpans(nowMs int64) []TraceSpan {
	res := make([]TraceSpan, 0, len(t.ActiveSpans))
	for _, span := range t.ActiveSpans {
		s := span
		if nowMs > s.StartMs {
			s.DurationMs = nowMs - s.StartMs
		} else {
			s.DurationMs = 0
		}
		if s.DurationMs > DefaultSpanAnomalyThresholdMs {
			s.IsAnomaly = true
			s.AnomalyMsg = AnomalyMsgStuckTool
		}
		res = append(res, s)
	}
	return res
}
