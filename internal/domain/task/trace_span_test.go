package task

import (
	"fmt"
	"sync"
	"testing"
)

// TestTraceSpan_StartEndDurationCalculation 测试工具调用从开始到结束的耗时计算与状态流转
func TestTraceSpan_StartEndDurationCalculation(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-span-calc",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Prompt:    "Run build and test suite",
		Timestamp: 1000, // 1,000,000 ms
	}, 1000000)

	// 1. 启动 Bash 工具调用
	startPayload := EventPayload{
		ID:        "sess-span-calc",
		Event:     "beforeShellExecution",
		ToolName:  "Bash",
		Detail:    "执行命令: go test ./...",
		Timestamp: 1001, // 1,001,000 ms
		SpanID:    "span-bash-1",
	}
	task.ApplyEvent(startPayload, 1001000, "10:00:01")

	if len(task.ActiveSpans) != 1 {
		t.Fatalf("expected 1 active span, got %d", len(task.ActiveSpans))
	}
	activeSpan, ok := task.ActiveSpans["span-bash-1"]
	if !ok {
		t.Fatalf("expected span-bash-1 in ActiveSpans")
	}
	if activeSpan.Status != SpanStatusRunning {
		t.Errorf("expected span status running, got %s", activeSpan.Status)
	}
	if activeSpan.StartMs != 1001000 {
		t.Errorf("expected startMs 1001000, got %d", activeSpan.StartMs)
	}

	// 2. 正常完成 Bash 工具调用（耗时 2500ms）
	endPayload := EventPayload{
		ID:        "sess-span-calc",
		Event:     "afterShellExecution",
		ToolName:  "Bash",
		Detail:    "执行命令完成: PASS",
		Timestamp: 1003, // 1003500 ms 传参精确到毫秒
		SpanID:    "span-bash-1",
	}
	task.ApplyEvent(endPayload, 1003500, "10:00:03")

	if len(task.ActiveSpans) != 0 {
		t.Errorf("expected 0 active spans after completion, got %d", len(task.ActiveSpans))
	}
	if len(task.TraceSpans) != 1 {
		t.Fatalf("expected 1 recorded trace span, got %d", len(task.TraceSpans))
	}
	completedSpan := task.TraceSpans[0]
	if completedSpan.Status != SpanStatusCompleted {
		t.Errorf("expected status completed, got %s", completedSpan.Status)
	}
	expectedDuration := int64(2500) // 1003500 - 1001000
	if completedSpan.DurationMs != expectedDuration {
		t.Errorf("expected duration %d ms, got %d ms", expectedDuration, completedSpan.DurationMs)
	}
	if completedSpan.EndMs != 1003500 {
		t.Errorf("expected endMs 1003500, got %d", completedSpan.EndMs)
	}

	// 3. 工具失败调用测试
	task.ApplyEvent(EventPayload{
		ID:        "sess-span-calc",
		Event:     "beforeToolUse",
		ToolName:  "Read",
		Detail:    "调用工具: Read config.yaml",
		Timestamp: 1005, // 1,005,000 ms
		SpanID:    "span-read-fail",
	}, 1005000, "10:00:05")

	task.ApplyEvent(EventPayload{
		ID:        "sess-span-calc",
		Event:     "toolFailure",
		ToolName:  "Read",
		Detail:    "工具执行失败 [Read]: file not found",
		Timestamp: 1006, // 1,006,200 ms
		SpanID:    "span-read-fail",
	}, 1006200, "10:00:06")

	if len(task.TraceSpans) != 2 {
		t.Fatalf("expected 2 trace spans, got %d", len(task.TraceSpans))
	}
	failedSpan := task.TraceSpans[1]
	if failedSpan.Status != SpanStatusFailed {
		t.Errorf("expected failed status, got %s", failedSpan.Status)
	}
	if failedSpan.DurationMs != 1200 {
		t.Errorf("expected duration 1200 ms, got %d ms", failedSpan.DurationMs)
	}
	// 工具失败不应使整个会话变为 failed
	if task.Status != "running" {
		t.Errorf("task should stay running on tool failure, got %s", task.Status)
	}
}

// TestTraceSpan_ClockSkewAndMonotonicAdjustment 测试时间戳乱序与时钟回拨的单调性保护
func TestTraceSpan_ClockSkewAndMonotonicAdjustment(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-clock-skew",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Prompt:    "Diagnose clock skew",
		Timestamp: 1000, // 1,000,000 ms
	}, 1000000)

	// 事件 1: 正常递增到 1,010,000 ms
	task.ApplyEvent(EventPayload{
		ID:        "sess-clock-skew",
		Event:     "beforeShellExecution",
		Detail:    "step 1",
		Timestamp: 1010, // 1,010,000 ms
	}, 1010000, "10:00:10")

	if task.LastTimelineTimestamp != 1010000 {
		t.Fatalf("expected LastTimelineTimestamp 1010000, got %d", task.LastTimelineTimestamp)
	}
	if task.ClockSkewCount != 0 {
		t.Fatalf("expected 0 clock skew count, got %d", task.ClockSkewCount)
	}

	// 事件 2: 客户端出现时钟回拨或乱序（Timestamp 为 1005 < 1010）
	task.ApplyEvent(EventPayload{
		ID:        "sess-clock-skew",
		Event:     "afterShellExecution",
		Detail:    "step 1 done out of order",
		Timestamp: 1005, // 1,005,000 ms (乱序回穿)
	}, 1005000, "10:00:05")

	if task.ClockSkewCount != 1 {
		t.Errorf("expected clockSkewCount 1, got %d", task.ClockSkewCount)
	}
	// 单调性保护：时间线时间戳不应回退
	if task.LastTimelineTimestamp < 1010000 {
		t.Errorf("lastTimelineTimestamp traveled backwards: %d", task.LastTimelineTimestamp)
	}

	timeline := task.Runs[0].Timeline
	if len(timeline) < 2 {
		t.Fatalf("expected at least 2 timeline items, got %d", len(timeline))
	}
	adjustedItem := timeline[len(timeline)-1]
	if !adjustedItem.ClockSkewAdjusted {
		t.Errorf("expected ClockSkewAdjusted to be true for out-of-order event")
	}
	if adjustedItem.Timestamp < 1010000 {
		t.Errorf("adjustedItem timestamp traveled backwards: %d", adjustedItem.Timestamp)
	}

	// 事件 3: 毫秒级时钟回拨
	task.ApplyEvent(EventPayload{
		ID:        "sess-clock-skew",
		Event:     "beforeToolUse",
		ToolName:  "Write",
		Detail:    "write file",
		Timestamp: 1000000, // 远早于当前已达到的 1,010,000
	}, 1000000, "10:00:00")

	if task.ClockSkewCount != 2 {
		t.Errorf("expected clockSkewCount 2, got %d", task.ClockSkewCount)
	}
}

// TestTraceSpan_ConcurrentSpansHandling 测试多并发/嵌套工具调用追踪与按工具/ID配对
func TestTraceSpan_ConcurrentSpansHandling(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-concurrent",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Prompt:    "Concurrent tool execution",
		Timestamp: 1000,
	}, 1000000)

	// 并发启动 Span A (Bash) 与 Span B (Read)
	task.ApplyEvent(EventPayload{
		ID:        "sess-concurrent",
		Event:     "beforeShellExecution",
		ToolName:  "Bash",
		Detail:    "npm run build",
		Timestamp: 1001,
		SpanID:    "span-bash",
	}, 1001000, "10:00:01")

	task.ApplyEvent(EventPayload{
		ID:        "sess-concurrent",
		Event:     "beforeToolUse",
		ToolName:  "Read",
		Detail:    "read package.json",
		Timestamp: 1002,
		SpanID:    "span-read",
	}, 1002000, "10:00:02")

	if len(task.ActiveSpans) != 2 {
		t.Fatalf("expected 2 active spans, got %d", len(task.ActiveSpans))
	}

	// Read 先完成 (耗时 500ms)
	task.ApplyEvent(EventPayload{
		ID:        "sess-concurrent",
		Event:     "afterToolUse",
		ToolName:  "Read",
		Detail:    "package.json read finished",
		Timestamp: 1002,
		SpanID:    "span-read",
	}, 1002500, "10:00:02")

	if len(task.ActiveSpans) != 1 {
		t.Fatalf("expected 1 active span remaining, got %d", len(task.ActiveSpans))
	}
	if _, ok := task.ActiveSpans["span-bash"]; !ok {
		t.Errorf("expected span-bash to remain active")
	}

	// Bash 后完成 (耗时 4000ms)
	task.ApplyEvent(EventPayload{
		ID:        "sess-concurrent",
		Event:     "afterShellExecution",
		ToolName:  "Bash",
		Detail:    "build success",
		Timestamp: 1005,
		SpanID:    "span-bash",
	}, 1005000, "10:00:05")

	if len(task.ActiveSpans) != 0 {
		t.Fatalf("expected 0 active spans, got %d", len(task.ActiveSpans))
	}
	if len(task.TraceSpans) != 2 {
		t.Fatalf("expected 2 completed trace spans, got %d", len(task.TraceSpans))
	}

	var bashSpan, readSpan TraceSpan
	for _, s := range task.TraceSpans {
		if s.SpanID == "span-bash" {
			bashSpan = s
		} else if s.SpanID == "span-read" {
			readSpan = s
		}
	}
	if bashSpan.DurationMs != 4000 {
		t.Errorf("expected bash span duration 4000ms, got %dms", bashSpan.DurationMs)
	}
	if readSpan.DurationMs != 500 {
		t.Errorf("expected read span duration 500ms, got %dms", readSpan.DurationMs)
	}
}

// TestTraceSpan_AnomalyDetection_LongRunning 测试超时与卡顿工具跨度的检测与诊断报告
func TestTraceSpan_AnomalyDetection_LongRunning(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-anomaly",
		Agent:     "Codex CLI",
		Event:     "sessionStart",
		Prompt:    "Long running background task",
		Timestamp: 1000,
	}, 1000000)

	// 启动一个长时间未结束的工具
	task.ApplyEvent(EventPayload{
		ID:        "sess-anomaly",
		Event:     "beforeShellExecution",
		ToolName:  "Bash",
		Detail:    "npm run dev --watch",
		Timestamp: 1000,
		SpanID:    "span-stuck-bash",
	}, 1000000, "10:00:00")

	// 运行 30 秒时检测：未超时 (30000 <= 60000)
	anomalies30s := task.DetectAnomalies(1030000)
	if len(anomalies30s) != 0 {
		t.Errorf("expected 0 anomalies at 30s, got %d", len(anomalies30s))
	}

	// 运行 70 秒时检测：触发异常 (> 60000)
	anomalies70s := task.DetectAnomalies(1070000)
	if len(anomalies70s) != 1 {
		t.Fatalf("expected 1 anomaly at 70s, got %d", len(anomalies70s))
	}
	anom := anomalies70s[0]
	if anom.SpanID != "span-stuck-bash" {
		t.Errorf("expected anomaly spanId span-stuck-bash, got %s", anom.SpanID)
	}
	if anom.ToolName != "Bash" {
		t.Errorf("expected toolName Bash, got %s", anom.ToolName)
	}
	if anom.DurationMs != 70000 {
		t.Errorf("expected duration 70000ms, got %dms", anom.DurationMs)
	}
	if anom.ThresholdMs != DefaultSpanAnomalyThresholdMs {
		t.Errorf("expected threshold %d, got %d", DefaultSpanAnomalyThresholdMs, anom.ThresholdMs)
	}
	if anom.AnomalyType != "stuck_tool" {
		t.Errorf("expected anomalyType stuck_tool, got %s", anom.AnomalyType)
	}

	// 验证 GetActiveTraceSpans 返回的实时状态
	activeSpans := task.GetActiveTraceSpans(1070000)
	if len(activeSpans) != 1 {
		t.Fatalf("expected 1 active span, got %d", len(activeSpans))
	}
	if !activeSpans[0].IsAnomaly {
		t.Errorf("expected active span IsAnomaly to be true")
	}
	if activeSpans[0].AnomalyMsg != AnomalyMsgStuckTool {
		t.Errorf("expected anomaly message %q, got %q", AnomalyMsgStuckTool, activeSpans[0].AnomalyMsg)
	}

	// 完成该工具执行（耗时 80 秒）
	task.ApplyEvent(EventPayload{
		ID:        "sess-anomaly",
		Event:     "afterShellExecution",
		ToolName:  "Bash",
		Detail:    "terminated",
		Timestamp: 1080,
		SpanID:    "span-stuck-bash",
	}, 1080000, "10:01:20")

	if len(task.TraceSpans) != 1 {
		t.Fatalf("expected 1 completed trace span, got %d", len(task.TraceSpans))
	}
	completed := task.TraceSpans[0]
	if !completed.IsAnomaly {
		t.Errorf("completed span should retain IsAnomaly=true due to exceeding threshold")
	}
	if completed.DurationMs != 80000 {
		t.Errorf("expected completed duration 80000ms, got %dms", completed.DurationMs)
	}
}

// TestTraceSpan_TaskClone_DeepCopyIsolation 严格验证 Task.Clone() 的全量 Span 深拷贝隔离
func TestTraceSpan_TaskClone_DeepCopyIsolation(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-clone-iso",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Prompt:    "Test deep clone isolation",
		Timestamp: 1000,
	}, 1000000)

	task.ApplyEvent(EventPayload{
		ID:        "sess-clone-iso",
		Event:     "beforeShellExecution",
		ToolName:  "Bash",
		Detail:    "active tool",
		Timestamp: 1001,
		SpanID:    "span-orig",
	}, 1001000, "10:00:01")

	// 生成克隆副本
	clone := task.Clone()
	if clone == nil {
		t.Fatalf("clone returned nil")
	}

	// 1. 验证修改 clone 的 ActiveSpans 不影响原任务
	clone.ActiveSpans["span-clone-only"] = TraceSpan{
		SpanID:   "span-clone-only",
		ToolName: "CloneTool",
	}
	if _, exists := task.ActiveSpans["span-clone-only"]; exists {
		t.Errorf("mutating clone ActiveSpans leaked to original task!")
	}

	// 2. 验证修改原任务的 ActiveSpans 不影响 clone
	task.ActiveSpans["span-orig-extra"] = TraceSpan{
		SpanID:   "span-orig-extra",
		ToolName: "ExtraTool",
	}
	if _, exists := clone.ActiveSpans["span-orig-extra"]; exists {
		t.Errorf("mutating original ActiveSpans leaked to clone task!")
	}

	// 3. 验证 TraceSpans 切片及内部元素相互隔离
	clone.TraceSpans = append(clone.TraceSpans, TraceSpan{
		SpanID:   "span-trace-clone",
		ToolName: "Write",
	})
	if len(task.TraceSpans) == len(clone.TraceSpans) {
		t.Errorf("mutating clone TraceSpans slice leaked to original task!")
	}

	// 4. 验证 Runs 内的 TraceSpans 深度隔离
	if len(clone.Runs) > 0 {
		clone.Runs[0].TraceSpans = append(clone.Runs[0].TraceSpans, TraceSpan{
			SpanID: "span-run-clone",
		})
		if len(task.Runs[0].TraceSpans) == len(clone.Runs[0].TraceSpans) {
			t.Errorf("mutating clone Runs[0].TraceSpans leaked to original task!")
		}
	}
}

// TestTraceSpan_CloseLingeringSpansOnSessionComplete 验证会话收口时活跃 span 自动收口
func TestTraceSpan_CloseLingeringSpansOnSessionComplete(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-lingering",
		Agent:     "Claude Code",
		Event:     "sessionStart",
		Prompt:    "Lingering span test",
		Timestamp: 1000,
	}, 1000000)

	task.ApplyEvent(EventPayload{
		ID:        "sess-lingering",
		Event:     "beforeToolUse",
		ToolName:  "Edit",
		Detail:    "edit main.go",
		Timestamp: 1001,
		SpanID:    "span-edit",
	}, 1001000, "10:00:01")

	if len(task.ActiveSpans) != 1 {
		t.Fatalf("expected 1 active span, got %d", len(task.ActiveSpans))
	}

	// 收到会话终态收口 agentCompletion
	task.ApplyEvent(EventPayload{
		ID:        "sess-lingering",
		Event:     "agentCompletion",
		Detail:    "Task completed",
		Timestamp: 1005,
	}, 1005000, "10:00:05")

	if len(task.ActiveSpans) != 0 {
		t.Errorf("expected 0 active spans after session completion, got %d", len(task.ActiveSpans))
	}
	if len(task.TraceSpans) != 1 {
		t.Fatalf("expected 1 recorded trace span, got %d", len(task.TraceSpans))
	}
	span := task.TraceSpans[0]
	if span.Status != SpanStatusCompleted {
		t.Errorf("expected status completed, got %s", span.Status)
	}
	if span.DurationMs != 4000 {
		t.Errorf("expected duration 4000ms, got %dms", span.DurationMs)
	}
}

// TestTraceSpan_EndWithoutStartGracefulHandling 验证后置钩子晚到或缺失前置钩子时的容错性
func TestTraceSpan_EndWithoutStartGracefulHandling(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-nostart",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Prompt:    "Orphan end hook test",
		Timestamp: 1000,
	}, 1000000)

	// 直接到达 postToolUse
	task.ApplyEvent(EventPayload{
		ID:        "sess-nostart",
		Event:     "postToolUse",
		ToolName:  "Grep",
		Detail:    "grep pattern in files",
		Timestamp: 1002,
		SpanID:    "span-grep-orphan",
	}, 1002000, "10:00:02")

	if len(task.TraceSpans) != 1 {
		t.Fatalf("expected 1 trace span recorded, got %d", len(task.TraceSpans))
	}
	span := task.TraceSpans[0]
	if span.SpanID != "span-grep-orphan" {
		t.Errorf("expected spanId span-grep-orphan, got %s", span.SpanID)
	}
	if span.ToolName != "Grep" {
		t.Errorf("expected toolName Grep, got %s", span.ToolName)
	}
	if span.Status != SpanStatusCompleted {
		t.Errorf("expected status completed, got %s", span.Status)
	}
}

// TestTraceSpan_ConcurrentAccess_RaceFree 测试多协程并发查询与状态变更下的 Race-Free
func TestTraceSpan_ConcurrentAccess_RaceFree(t *testing.T) {
	task := NewTask(EventPayload{
		ID:        "sess-race-free",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Prompt:    "High concurrency span test",
		Timestamp: 1000,
	}, 1000000)

	var mu sync.RWMutex
	var wg sync.WaitGroup

	// 并发写与读
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mu.Lock()
			spanID := fmt.Sprintf("span-conc-%d", idx)
			task.ApplyEvent(EventPayload{
				ID:        "sess-race-free",
				Event:     "beforeShellExecution",
				ToolName:  "Bash",
				Detail:    fmt.Sprintf("cmd-%d", idx),
				Timestamp: int64(1000 + idx),
				SpanID:    spanID,
			}, int64(1000000+idx*100), "10:00:00")
			task.ApplyEvent(EventPayload{
				ID:        "sess-race-free",
				Event:     "afterShellExecution",
				ToolName:  "Bash",
				Detail:    fmt.Sprintf("cmd-%d-done", idx),
				Timestamp: int64(1001 + idx),
				SpanID:    spanID,
			}, int64(1001000+idx*100), "10:00:01")
			mu.Unlock()
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.RLock()
			clone := task.Clone()
			_ = clone.GetActiveTraceSpans(1010000)
			mu.RUnlock()
		}()
	}

	wg.Wait()

	if len(task.TraceSpans) != 20 {
		t.Errorf("expected 20 trace spans, got %d", len(task.TraceSpans))
	}
}
