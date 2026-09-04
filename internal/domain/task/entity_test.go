package task

import (
	"fmt"
	"strings"
	"testing"
)

func TestTask_LifecycleAndDomainRules(t *testing.T) {
	// 1. Initial creation with placeholder
	p1 := EventPayload{
		ID:        "sess-domain-1",
		Agent:     "ZCode",
		Repo:      "my/repo",
		Branch:    "master",
		Event:     "SessionStart",
		Timestamp: 1000,
	}

	task := NewTask(p1, 1000000)
	if task.Title != "ZCode 任务" || task.RootGoal != "ZCode 任务" {
		t.Fatalf("expected placeholder title, got title=%q rootGoal=%q", task.Title, task.RootGoal)
	}
	if len(task.Runs) != 1 || task.Runs[0].Status != "running" {
		t.Fatalf("expected 1 running turn")
	}

	// 2. Real prompt submitted in turn 1
	p2 := EventPayload{
		ID:        "sess-domain-1",
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    "#task 按照 DDD 重构代码结构\n补充说明：分成 domain/app/infra",
		Timestamp: 1005,
		Detail:    "Prompt arrived",
	}
	task.ApplyEvent(p2, 1005000, "10:00:05")

	expectedTitle := "按照 DDD 重构代码结构"
	if task.Title != expectedTitle {
		t.Errorf("expected Title=%q, got %q", expectedTitle, task.Title)
	}
	if task.RootGoal != p2.Prompt {
		t.Errorf("expected RootGoal=%q, got %q", p2.Prompt, task.RootGoal)
	}

	// 3. Complete turn 1 with AIResponse
	p3 := EventPayload{
		ID:         "sess-domain-1",
		Agent:      "ZCode",
		Event:      "agentCompletion",
		Timestamp:  1010,
		Detail:     "Finished turn 1",
		AIResponse: "重构已完成，划分为了领域层、应用层与基础设施层。",
	}
	task.ApplyEvent(p3, 1010000, "10:00:10")
	if task.Status != "completed" || task.Runs[0].Status != "completed" {
		t.Fatalf("expected completed status")
	}
	if task.Runs[0].AIResponse != p3.AIResponse {
		t.Errorf("expected AIResponse %q, got %q", p3.AIResponse, task.Runs[0].AIResponse)
	}

	// Test timeline deduplication
	initialTimelineLen := len(task.Runs[0].Timeline)
	duplicateEvent := EventPayload{
		ID:        "sess-domain-1",
		Agent:     "ZCode",
		Event:     "agentCompletion",
		Timestamp: 1010,
		Detail:    "Finished turn 1",
	}
	task.ApplyEvent(duplicateEvent, 1010000, "10:00:10")
	if len(task.Runs[0].Timeline) != initialTimelineLen {
		t.Errorf("expected timeline deduplication, len was %d, now %d", initialTimelineLen, len(task.Runs[0].Timeline))
	}

	// 4. Start turn 2
	p4 := EventPayload{
		ID:        "sess-domain-1",
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    "继续执行单元测试",
		Timestamp: 1020,
	}
	task.ApplyEvent(p4, 1020000, "10:00:20")
	if len(task.Runs) != 2 || task.TotalRuns != 2 {
		t.Fatalf("expected 2 runs, got %d", len(task.Runs))
	}
	if task.Status != "running" || task.Runs[1].Status != "running" {
		t.Fatalf("expected turn 2 running")
	}
	if task.Runs[1].Title != "继续执行单元测试" {
		t.Errorf("expected run 2 title '继续执行单元测试', got %q", task.Runs[1].Title)
	}
	if task.Title != "继续执行单元测试" {
		t.Errorf("expected session Title to follow latest prompt, got %q", task.Title)
	}
	if task.RootGoal != p2.Prompt {
		t.Errorf("RootGoal must stay first-round prompt, got %q", task.RootGoal)
	}

	summarized := "DDD 重构并补齐单测"
	if !task.ApplyDisplayTitle(summarized) {
		t.Fatal("ApplyDisplayTitle should accept a real summary")
	}
	if task.Title != summarized || task.TitleSource != "llm" {
		t.Errorf("ApplyDisplayTitle = title=%q source=%q", task.Title, task.TitleSource)
	}
	if task.RootGoal != p2.Prompt {
		t.Errorf("ApplyDisplayTitle must not rewrite RootGoal, got %q", task.RootGoal)
	}

	task.refreshHeuristicTitle("这条启发式不应覆盖 LLM 标题")
	if task.Title != summarized {
		t.Errorf("LLM title must not be overwritten by heuristic, got %q", task.Title)
	}
	if task.ApplyDisplayTitle("") || task.ApplyDisplayTitle("ZCode 任务") {
		t.Error("ApplyDisplayTitle must reject empty/placeholder titles")
	}

	goal := "跨轮完成 DDD 分层，并补齐领域单测。"
	if !task.ApplyGoalSummary(goal, 2) {
		t.Fatal("ApplyGoalSummary should accept a real summary")
	}
	if task.GoalSummary != goal || task.GoalSummaryRun != 2 {
		t.Errorf("ApplyGoalSummary = summary=%q run=%d", task.GoalSummary, task.GoalSummaryRun)
	}
	if task.RootGoal != p2.Prompt {
		t.Errorf("ApplyGoalSummary must not rewrite RootGoal, got %q", task.RootGoal)
	}
	if task.ApplyGoalSummary("", 2) || task.ApplyGoalSummary("ZCode 任务", 2) || task.ApplyGoalSummary(goal, 0) {
		t.Error("ApplyGoalSummary must reject empty/placeholder/invalid run")
	}

	// 5. Test transient tool failure (should NOT terminate task)
	pFail := EventPayload{
		ID:        "sess-domain-1",
		Event:     "postToolUseFailure",
		Detail:    "grep exit code 1",
		Timestamp: 1025,
	}
	task.ApplyEvent(pFail, 1025000, "10:00:25")
	if task.Status != "running" || task.Runs[1].Status != "running" {
		t.Fatalf("transient tool failure should keep task in running state, got task.Status=%s", task.Status)
	}

	// 6. Test Task Clone deep copy
	clone := task.Clone()
	if clone.ID != task.ID || len(clone.Runs) != len(task.Runs) {
		t.Fatalf("clone failed to copy properties")
	}
	clone.Runs[0].Title = "Modified in clone"
	if task.Runs[0].Title == "Modified in clone" {
		t.Fatalf("clone was not a deep copy")
	}
}

func TestTask_ApplyDisplayTitleSanitizes(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-title-sanitize",
		Agent:  "ZCode",
		Event:  "SessionStart",
		Prompt: "#task 原始长提示词\n第二行",
	}, 1000000)

	if !task.ApplyDisplayTitle("「会话标题总结」\n后面这行应丢弃") {
		t.Fatal("expected sanitized title to apply")
	}
	if task.Title != "会话标题总结" {
		t.Errorf("sanitized title = %q", task.Title)
	}
	long := strings.Repeat("标", 80)
	if !task.ApplyDisplayTitle(long) {
		t.Fatal("expected long title to truncate")
	}
	if got := []rune(task.Title); len(got) != 48 {
		t.Errorf("expected 48 runes, got %d", len(got))
	}
}

func TestTask_ApplyGoalSummarySanitizes(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-goal-sanitize",
		Agent:  "ZCode",
		Event:  "SessionStart",
		Prompt: "#task 原始长提示词\n第二行",
	}, 1000000)
	root := task.RootGoal

	if !task.ApplyGoalSummary("「跨轮把支付模块拆到 domain，并补接口测试。」\n", 3) {
		t.Fatal("expected sanitized goal to apply")
	}
	if task.GoalSummary != "跨轮把支付模块拆到 domain，并补接口测试。" {
		t.Errorf("sanitized goal = %q", task.GoalSummary)
	}
	if task.RootGoal != root {
		t.Errorf("RootGoal rewritten: %q", task.RootGoal)
	}
	long := strings.Repeat("标", 300)
	if !task.ApplyGoalSummary(long, 6) {
		t.Fatal("expected long goal to truncate")
	}
	if got := []rune(task.GoalSummary); len(got) != 240 {
		t.Errorf("expected 240 runes, got %d", len(got))
	}
	if task.GoalSummaryRun != 6 {
		t.Errorf("GoalSummaryRun = %d", task.GoalSummaryRun)
	}
}

func TestService_Helpers(t *testing.T) {
	if !IsPlaceholderTitle("未命名") || !IsPlaceholderTitle("ZCode 任务") || !IsPlaceholderTitle("CLI Task 123") {
		t.Errorf("placeholder detection failed")
	}
	if IsPlaceholderTitle("实现支付模块") {
		t.Errorf("false positive on placeholder detection")
	}
	if FormatDuration(65) != "01m 05s" {
		t.Errorf("FormatDuration error: got %s", FormatDuration(65))
	}
	cleaned := CleanPromptTitle("#task [board]  修复 Bug \n第二行")
	if cleaned != "修复 Bug" {
		t.Errorf("CleanPromptTitle failed: %q", cleaned)
	}
}

func TestTask_CursorStopCompletes(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-cursor-1",
		Agent:  "Cursor Agent",
		Event:  "sessionStart",
		Prompt: "修复 Cursor 适配",
	}, 1000000)

	task.ApplyEvent(EventPayload{
		ID:         "sess-cursor-1",
		Event:      "stop",
		AIResponse: "适配已修好。",
		Detail:     "任务执行完成",
	}, 1010000, "10:00:10")

	if task.Status != "completed" || task.Runs[0].Status != "completed" {
		t.Fatalf("Cursor stop should complete the run, got task=%s run=%s", task.Status, task.Runs[0].Status)
	}
	if task.Runs[0].AIResponse != "适配已修好。" {
		t.Errorf("AIResponse = %q", task.Runs[0].AIResponse)
	}

	failed := NewTask(EventPayload{
		ID:    "sess-cursor-2",
		Agent: "Cursor Agent",
		Event: "sessionStart",
	}, 2000000)
	failed.ApplyEvent(EventPayload{
		ID:     "sess-cursor-2",
		Event:  "failed",
		Detail: "aborted",
	}, 2010000, "10:00:20")
	if failed.Status != "failed" {
		t.Fatalf("mapped aborted stop should fail, got %s", failed.Status)
	}
}

func TestTask_AfterAgentResponseClosesRunWithoutStop(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-ai-only",
		Agent:  "Cursor Agent",
		Event:  "sessionStart",
		Prompt: "是不是需要支持只统计指定会话和任务呢？",
	}, 1000000)
	task.ApplyEvent(EventPayload{
		ID:     "sess-ai-only",
		Event:  "sessionStart",
		Prompt: "是不是需要支持只统计指定会话和任务呢？",
		Detail: "会话启动，分析任务中...",
	}, 1000000, "02:21:14")

	task.ApplyEvent(EventPayload{
		ID:         "sess-ai-only",
		Event:      "afterAgentResponse",
		Detail:     "AI 回复: 顶栏统计跟当前视野走即可。",
		AIResponse: "顶栏统计跟当前视野走即可。",
	}, 1010000, "02:22:40")

	if task.Status != "completed" || task.Runs[0].Status != "completed" {
		t.Fatalf("afterAgentResponse should close the run when stop is missing, got task=%s run=%s", task.Status, task.Runs[0].Status)
	}
	if task.Runs[0].AIResponse != "顶栏统计跟当前视野走即可。" {
		t.Errorf("AIResponse = %q", task.Runs[0].AIResponse)
	}
	if task.Runs[0].Duration == "" {
		t.Fatal("closed run should record duration")
	}

	task.ApplyEvent(EventPayload{
		ID:     "sess-ai-only",
		Event:  "agentCompletion",
		Detail: "任务执行完成",
	}, 1011000, "02:22:41")
	if task.Status != "completed" || task.Runs[0].Status != "completed" {
		t.Fatalf("late stop must stay completed, got task=%s run=%s", task.Status, task.Runs[0].Status)
	}
}

func TestTask_CursorAfterAgentResponseAndStopDedupsAIReply(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-cursor-dup",
		Agent:  "Cursor Agent",
		Event:  "sessionStart",
		Prompt: "启动一下 8000",
	}, 1000000)

	reply := "AI 回复: **Agent Monitor 已在 8000 端口启动。**"
	task.ApplyEvent(EventPayload{
		ID:         "sess-cursor-dup",
		Event:      "afterAgentResponse",
		Detail:     reply,
		AIResponse: "**Agent Monitor 已在 8000 端口启动。**",
	}, 1010000, "02:03:36")
	task.ApplyEvent(EventPayload{
		ID:         "sess-cursor-dup",
		Event:      "agentCompletion",
		Detail:     reply,
		AIResponse: "**Agent Monitor 已在 8000 端口启动。**",
	}, 1010000, "02:03:36")

	tl := task.Runs[0].Timeline
	aiCount := 0
	for _, item := range tl {
		if item.Desc == reply {
			aiCount++
		}
	}
	if aiCount != 1 {
		t.Fatalf("expected 1 AI reply breadcrumb, got %d in %+v", aiCount, tl)
	}
	if task.Status != "completed" || task.Runs[0].Status != "completed" {
		t.Fatalf("stop should still complete the run, got task=%s run=%s", task.Status, task.Runs[0].Status)
	}
}

func TestTask_DoesNotSplitFirstRunOnInflatedTurnIndex(t *testing.T) {
	p := EventPayload{
		ID:        "sess-init",
		Event:     "sessionStart",
		Prompt:    "第一轮问题",
		TurnIndex: 3,
	}
	task := NewTask(p, 1000000)
	task.ApplyEvent(p, 1000000, "10:00:00")
	if len(task.Runs) != 1 {
		t.Fatalf("first event must stay on run 1, got %d runs", len(task.Runs))
	}
	if task.Runs[0].Status != "running" {
		t.Fatalf("run 1 status = %s", task.Runs[0].Status)
	}
}

func TestTask_ToolEventCannotJumpTurn(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-tool",
		Event:  "sessionStart",
		Prompt: "问 A",
	}, 1000000)
	task.ApplyEvent(EventPayload{
		ID:        "sess-tool",
		Event:     "sessionStart",
		Prompt:    "问 A",
		TurnIndex: 1,
		Detail:    "start",
	}, 1000000, "10:00:00")
	task.ApplyEvent(EventPayload{
		ID:        "sess-tool",
		Event:     "beforeShellExecution",
		Detail:    "ls",
		TurnIndex: 3,
	}, 1005000, "10:00:05")
	if len(task.Runs) != 1 {
		t.Fatalf("tool event must not open a new run, got %d", len(task.Runs))
	}
	if task.Runs[0].Status != "running" {
		t.Fatalf("run status = %s", task.Runs[0].Status)
	}
}

func TestTask_NewPromptWhileRunningClosesPrevious(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-gap",
		Event:  "sessionStart",
		Prompt: "问 A",
	}, 1000000)
	task.ApplyEvent(EventPayload{
		ID:        "sess-gap",
		Event:     "sessionStart",
		Prompt:    "问 A",
		TurnIndex: 1,
		Detail:    "start",
	}, 1000000, "10:00:00")
	task.ApplyEvent(EventPayload{
		ID:        "sess-gap",
		Event:     "beforeShellExecution",
		Detail:    "grep",
		TurnIndex: 1,
	}, 1010000, "10:00:10")

	task.ApplyEvent(EventPayload{
		ID:        "sess-gap",
		Event:     "sessionStart",
		Prompt:    "问 B",
		TurnIndex: 2,
		Detail:    "next prompt",
	}, 1020000, "10:00:20")

	if len(task.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(task.Runs))
	}
	if task.Runs[0].Status != "completed" {
		t.Fatalf("run 1 should be auto-closed, got %s", task.Runs[0].Status)
	}
	if task.Runs[0].Duration == "" {
		t.Fatalf("auto-closed run should record duration")
	}
	if task.Runs[1].Status != "running" || task.Runs[1].Prompt != "问 B" {
		t.Fatalf("run 2 should be running with new prompt, status=%s prompt=%q", task.Runs[1].Status, task.Runs[1].Prompt)
	}

	task.ApplyEvent(EventPayload{
		ID:        "sess-gap",
		Event:     "agentCompletion",
		Detail:    "done",
		TurnIndex: 3,
	}, 1030000, "10:00:30")
	if task.Status != "completed" {
		t.Fatalf("task should complete, got %s", task.Status)
	}
	if task.Runs[0].Status != "completed" || task.Runs[1].Status != "completed" {
		t.Fatalf("no run should remain running, got %s / %s", task.Runs[0].Status, task.Runs[1].Status)
	}
}

func TestTask_NewPromptWithoutTurnIndexStillAdvances(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-prompt",
		Event:  "sessionStart",
		Prompt: "问 A",
	}, 1000000)
	task.ApplyEvent(EventPayload{
		ID:     "sess-prompt",
		Event:  "sessionStart",
		Prompt: "问 A",
		Detail: "start",
	}, 1000000, "10:00:00")
	task.ApplyEvent(EventPayload{
		ID:     "sess-prompt",
		Event:  "UserPromptSubmit",
		Prompt: "问 B",
		Detail: "follow-up",
	}, 1020000, "10:00:20")

	if len(task.Runs) != 2 {
		t.Fatalf("expected 2 runs from changed prompt, got %d", len(task.Runs))
	}
	if task.Runs[0].Status != "completed" || task.Runs[1].Status != "running" {
		t.Fatalf("previous run should close, got %s / %s", task.Runs[0].Status, task.Runs[1].Status)
	}
}

func TestTask_CloseOrphanRunsHealsMatrixHole(t *testing.T) {
	task := NewTask(EventPayload{
		ID:     "sess-hole",
		Event:  "sessionStart",
		Prompt: "问 A",
	}, 1000000)
	task.Runs = []Turn{
		{Index: 1, Prompt: "问 A", Status: "completed", StartTime: 1000000, EndTime: 1010000, Duration: "00m 10s", Timeline: []TimelineItem{}},
		{Index: 2, Prompt: "问 B", Status: "running", StartTime: 1020000, Timeline: []TimelineItem{}},
		{Index: 3, Prompt: "问 C", Status: "completed", StartTime: 1030000, EndTime: 1040000, Duration: "00m 10s", Timeline: []TimelineItem{}},
	}
	task.TotalRuns = 3
	task.Status = "completed"

	if !task.CloseOrphanRuns(1050000, "10:00:50") {
		t.Fatal("expected hole to be healed")
	}
	if task.Runs[1].Status != "completed" {
		t.Fatalf("run 2 should be closed, got %s", task.Runs[1].Status)
	}
	if task.Runs[1].EndTime != 1030000 {
		t.Fatalf("run 2 should end when run 3 started, got %d", task.Runs[1].EndTime)
	}

	// 后续完成事件不能再把中间轮打回 running
	task.ApplyEvent(EventPayload{
		ID:        "sess-hole",
		Event:     "agentCompletion",
		Detail:    "already done",
		TurnIndex: 3,
	}, 1060000, "10:01:00")
	if task.Runs[0].Status != "completed" || task.Runs[1].Status != "completed" || task.Runs[2].Status != "completed" {
		t.Fatalf("matrix hole returned: %s / %s / %s", task.Runs[0].Status, task.Runs[1].Status, task.Runs[2].Status)
	}
}

func TestTask_VacuousLifecycleAndHasUserWork(t *testing.T) {
	emptyStart := EventPayload{ID: "sess-empty", Agent: "Cursor Agent", Event: "sessionStart"}
	if !IsVacuousLifecycle(emptyStart) {
		t.Fatal("empty sessionStart should be vacuous")
	}
	if !IsVacuousLifecycle(EventPayload{ID: "sess-empty", Event: "agentCompletion"}) {
		t.Fatal("empty sessionEnd/agentCompletion should be vacuous")
	}
	if IsVacuousLifecycle(EventPayload{ID: "sess-empty", Event: "sessionStart", Prompt: "修 bug"}) {
		t.Fatal("sessionStart with prompt is real work")
	}
	if IsVacuousLifecycle(EventPayload{ID: "sess-empty", Event: "sessionStart", Title: "Connection Test"}) {
		t.Fatal("sessionStart with a real title is intentional")
	}
	if IsVacuousLifecycle(EventPayload{ID: "sess-empty", Event: "beforeShellExecution", Detail: "ls"}) {
		t.Fatal("tool events are not lifecycle shells")
	}

	idle := NewTask(emptyStart, 1000000)
	idle.ApplyEvent(EventPayload{ID: "sess-empty", Event: "sessionStart", Detail: "会话启动，分析任务中..."}, 1000000, "10:00:00")
	if idle.HasUserWork() {
		t.Fatal("placeholder open-and-close session has no user work")
	}

	worked := NewTask(EventPayload{ID: "sess-work", Event: "sessionStart", Prompt: "修 bug"}, 1000000)
	if !worked.HasUserWork() {
		t.Fatal("prompted session has user work")
	}

	tools := NewTask(EventPayload{ID: "sess-tool", Agent: "Cursor Agent", Event: "beforeShellExecution"}, 1000000)
	tools.ApplyEvent(EventPayload{ID: "sess-tool", Event: "beforeShellExecution", Detail: "ls"}, 1000000, "10:00:00")
	if !tools.HasUserWork() {
		t.Fatal("tool-only session has user work")
	}
}

func TestTask_SubagentHierarchyAndClone(t *testing.T) {
	// 1. 父任务启动，派发子任务
	parent := NewTask(EventPayload{
		ID:     "parent-task-100",
		Agent:  "ZCode",
		Event:  "sessionStart",
		Prompt: "#task 主流程重构",
	}, 1000000)

	if parent.SubagentCount != 0 {
		t.Fatalf("expected subagent count 0, got %d", parent.SubagentCount)
	}

	// 2. 派发子智能体事件
	parent.ApplyEvent(EventPayload{
		ID:           "parent-task-100",
		Event:        "subagentStart",
		Detail:       "派发子智能体 [Explore]: 检索代码库并发模型",
		SubagentType: "Explore",
		SubagentID:   "agent_explore_01",
	}, 1010000, "10:01:00")

	if parent.SubagentCount != 1 {
		t.Fatalf("expected subagent count 1, got %d", parent.SubagentCount)
	}

	curTimeline := parent.Runs[0].Timeline
	if len(curTimeline) != 1 {
		t.Fatalf("expected 1 timeline event, got %d", len(curTimeline))
	}
	if curTimeline[0].SubagentType != "Explore" || curTimeline[0].SubagentID != "agent_explore_01" {
		t.Fatalf("unexpected timeline subagent metadata: %+v", curTimeline[0])
	}

	// 3. 子任务以独立 session 启动，携带 ParentID 和自身角色 SubagentType
	child := NewTask(EventPayload{
		ID:           "child-task-200",
		ParentID:     "parent-task-100",
		SubagentType: "Explore",
		Agent:        "ZCode",
		Event:        "sessionStart",
		Prompt:       "检索代码库并发模型",
	}, 1020000)

	if child.ParentID != "parent-task-100" {
		t.Fatalf("expected ParentID parent-task-100, got %s", child.ParentID)
	}
	if child.SubagentCount != 0 {
		t.Fatalf("child subagent itself should have SubagentCount 0, got %d", child.SubagentCount)
	}

	// 4. 子任务自身执行多次常规工具调用，确保其 SubagentCount 不会虚高自增
	for i := 1; i <= 5; i++ {
		child.ApplyEvent(EventPayload{
			ID:           "child-task-200",
			ParentID:     "parent-task-100",
			SubagentType: "Explore",
			Event:        "beforeShellExecution",
			Detail:       fmt.Sprintf("grep code pattern %d", i),
		}, int64(1020000+i*1000), fmt.Sprintf("10:02:0%d", i))
	}
	if child.SubagentCount != 0 {
		t.Fatalf("child tool execution should not increment SubagentCount, got %d", child.SubagentCount)
	}
	if parent.SubagentCount != 1 {
		t.Fatalf("parent SubagentCount should stay 1, got %d", parent.SubagentCount)
	}

	// 5. 验证子任务自身继续派发二级子代理（多级嵌套），子任务自身的 SubagentCount 应正常递增为 1
	child.ApplyEvent(EventPayload{
		ID:           "child-task-200",
		ParentID:     "parent-task-100",
		Event:        "subagentStart",
		Detail:       "子代理进一步派发 [Reviewer]: 审查并发竞态分析结果",
		SubagentType: "Reviewer",
		SubagentID:   "agent_reviewer_02",
	}, 1030000, "10:03:00")

	if child.SubagentCount != 1 {
		t.Fatalf("hierarchical subagent: expected child SubagentCount 1, got %d", child.SubagentCount)
	}

	// 重复事件防抖判定，不重复计数
	child.ApplyEvent(EventPayload{
		ID:           "child-task-200",
		ParentID:     "parent-task-100",
		Event:        "subagentStart",
		Detail:       "子代理进一步派发 [Reviewer]: 审查并发竞态分析结果",
		SubagentType: "Reviewer",
		SubagentID:   "agent_reviewer_02",
	}, 1030000, "10:03:00")

	if child.SubagentCount != 1 {
		t.Fatalf("subagentCount idempotency: duplicate event should not increment, got %d", child.SubagentCount)
	}

	// 6. 验证常规工具事件不会篡改 Task.ParentID
	parent.ApplyEvent(EventPayload{
		ID:       "parent-task-100",
		ParentID: "rogue-parent-id",
		Event:    "beforeShellExecution",
		Detail:   "npm test",
	}, 1040000, "10:04:00")
	if parent.ParentID != "" {
		t.Fatalf("tool event must not overwrite empty ParentID on root task, got %q", parent.ParentID)
	}

	// 7. Task.Clone() 深拷贝验证
	clone := parent.Clone()
	if clone.SubagentCount != parent.SubagentCount || clone.ParentID != parent.ParentID {
		t.Fatalf("clone failed on subagent fields")
	}
	clone.SubagentCount = 99
	if parent.SubagentCount == 99 {
		t.Fatalf("clone mutated parent SubagentCount")
	}
}

func TestTaskKeyIDImmutability(t *testing.T) {
	task := NewTask(EventPayload{
		ID:    "task-tenant-lock-1",
		KeyID: "project-alpha",
		Event: "sessionStart",
		Title: "Alpha Task",
	}, 1000000)

	if task.KeyID != "project-alpha" {
		t.Fatalf("expected initial KeyID 'project-alpha', got %q", task.KeyID)
	}

	// 尝试通过后续事件篡改 KeyID
	task.ApplyEvent(EventPayload{
		ID:     "task-tenant-lock-1",
		KeyID:  "project-hacker",
		Event:  "toolUse",
		Detail: "echo hack",
	}, 1001000, "10:01:00")

	if task.KeyID != "project-alpha" {
		t.Fatalf("KeyID was illegally overwritten: expected 'project-alpha', got %q", task.KeyID)
	}
}
