package task

import (
	"fmt"
	"strings"
	"time"
)

// TimelineItem 记录任务时间轴上的单条事件（值对象）。
type TimelineItem struct {
	Time         string `json:"time"`                   // 事件时间，格式 HH:MM:SS
	Event        string `json:"event"`                  // hook 事件名
	Desc         string `json:"desc"`                   // 事件描述
	SubagentType string `json:"subagentType,omitempty"` // 子智能体角色类型 (如 Explore / judge / general)
	SubagentID   string `json:"subagentId,omitempty"`   // 子智能体 ID
}

// Turn 表示单个会话内的独立一轮执行周期（Run 实体）。
type Turn struct {
	Index      int            `json:"index"`                // 轮次序号 1, 2, 3...
	Prompt     string         `json:"prompt,omitempty"`     // 当轮 Prompt 正文
	Title      string         `json:"title,omitempty"`      // 当轮标题
	AIResponse string         `json:"aiResponse,omitempty"` // 当轮 AI 最终回复与总结
	Status     string         `json:"status"`               // running / completed / failed
	StartTime  int64          `json:"startTime"`            // 当轮开始时间，Unix 毫秒
	EndTime    int64          `json:"endTime,omitempty"`    // 当轮结束时间，Unix 毫秒
	Duration   string         `json:"duration,omitempty"`   // 当轮实际执行耗时
	Detail     string         `json:"detail,omitempty"`     // 当轮最新操作描述
	LastHook   string         `json:"lastHook,omitempty"`   // 当轮最后一次 Hook 事件
	Timeline   []TimelineItem `json:"timeline"`             // 当轮独立 Hook 轨迹
}

// Task 表示 Monitor 上的一个 Agent 会话容器（聚合根 Workflow）。
type Task struct {
	ID             string         `json:"id"`                       // 会话 ID (如 sess_xxx)
	Agent          string         `json:"agent"`                    // Agent 名称
	Repo           string         `json:"repo"`                     // 仓库名与分支
	Branch         string         `json:"branch"`                   // 分支名
	Event          string         `json:"event"`                    // 最近一次事件名
	RootGoal       string         `json:"rootGoal"`                 // 会话核心总目标（首轮 Prompt 原文，不随 LLM 改写）
	GoalSummary    string         `json:"goalSummary,omitempty"`    // 多轮 LLM 会话总目标；空则 UI 回退 RootGoal
	GoalSummaryRun int            `json:"goalSummaryRun,omitempty"` // GoalSummary 覆盖到的轮次，避免重复请求
	Title          string         `json:"title"`                    // 会话容器展示标题（启发式短标题或 LLM 总结）
	TitleSource    string         `json:"titleSource,omitempty"`    // 标题来源：heuristic | llm
	Prompt         string         `json:"prompt,omitempty"`         // 首轮 Prompt（兼容字段）
	Status         string         `json:"status"`                   // 全局状态：running / completed / failed
	StartTime      int64          `json:"startTime"`                // 会话总创建时间戳（毫秒）
	EndTime        int64          `json:"endTime,omitempty"`        // 会话最终完结时间戳（毫秒）
	TotalLifetime  int64          `json:"totalLifetime,omitempty"`  // 累计所有已完成轮次的总有效执行秒数
	Duration       string         `json:"duration,omitempty"`       // 会话累计总有效耗时
	ActiveRunStart int64          `json:"activeRunStart,omitempty"` // 当前活跃 Run 的起始时间戳（毫秒）
	ActiveRunIndex int            `json:"activeRunIndex"`           // 当前活跃/最新 Run 序号
	TotalRuns      int            `json:"totalRuns"`                // 总 Run 轮数
	Runs           []Turn         `json:"runs"`                     // 所有轮次列表
	Turns          []Turn         `json:"turns,omitempty"`          // 兼容旧字段别名
	LastHook       string         `json:"lastHook"`                 // 最近一次 hook
	Detail         string         `json:"detail"`                   // 当前操作详情
	Timeline       []TimelineItem `json:"timeline,omitempty"`       // 兼容顶层时间线（映射为当轮）
	ControlState   string         `json:"controlState,omitempty"`   // 控制状态："" | "abort_requested" | "aborted"
	AbortReason    string         `json:"abortReason,omitempty"`    // 中断原因
	PID            int            `json:"pid,omitempty"`            // 关联的进程 ID
	PGID           int            `json:"pgid,omitempty"`           // 关联的进程组 ID
	KeyID          string         `json:"keyId,omitempty"`          // 归属的项目/租户空间标识
	ParentID       string         `json:"parentId,omitempty"`       // 父任务 ID (若当前为子代理会话)
	SubagentCount  int            `json:"subagentCount,omitempty"`  // 当前任务派发或关联的子智能体总数
	Version        uint64         `json:"version,omitempty"`        // 状态单调递增版本号（防磁盘乱序覆写）
}

// EventPayload 是 Hook 上报的数据传输对象 (DTO)。
type EventPayload struct {
	ID           string `json:"id"`                      // 会话/任务 ID，空则自动生成
	ParentID     string `json:"parent_id,omitempty"`     // 父任务 ID（可选）
	SubagentID   string `json:"subagent_id,omitempty"`   // 子智能体 ID（可选）
	SubagentType string `json:"subagent_type,omitempty"` // 子智能体类型（可选）
	Agent        string `json:"agent"`                   // Agent 名称
	Repo         string `json:"repo"`                    // 仓库信息
	Branch       string `json:"branch"`                  // 分支名
	Event        string `json:"event"`                   // hook 事件名，决定任务状态流转
	Title        string `json:"title"`                   // 任务标题
	Prompt       string `json:"prompt"`                  // 本轮 Prompt
	AIResponse   string `json:"ai_response,omitempty"`   // 本轮 AI 总结与回复
	Timestamp    int64  `json:"timestamp"`               // Unix 秒；为 0 则用服务端当前时间
	Detail       string `json:"detail"`                  // 本次操作的简要说明
	TurnIndex    int    `json:"turn_index,omitempty"`    // 上报指定的轮次（可选）
	PID          int    `json:"pid,omitempty"`           // 上报来源的进程 PID（可选）
	PGID         int    `json:"pgid,omitempty"`          // 上报来源的进程组 PGID（可选）
	KeyID        string `json:"key_id,omitempty"`        // 归属的项目/租户空间标识（可选）
}

// BelongsTo 检查该任务是否属于指定租户/Key空间（当 targetKey 为空或 isMaster 为 true 时放行）。
func (t *Task) BelongsTo(targetKey string, isMaster bool) bool {
	if isMaster || targetKey == "" || targetKey == "*" {
		return true
	}
	if t.KeyID == "" {
		return targetKey == "default" || targetKey == ""
	}
	return t.KeyID == targetKey
}

// NewTask 根据首个上报事件创建全新的 Task 聚合根。
func NewTask(p EventPayload, nowMs int64) *Task {
	if p.ID == "" {
		p.ID = fmt.Sprintf("sess-%d", time.Now().UnixNano()%100000)
	}
	if p.Agent == "" {
		p.Agent = "AI Agent"
	}

	title := p.Title
	if !IsRealTitle(title) {
		if p.Prompt != "" {
			title = CleanPromptTitle(p.Prompt)
		}
		if !IsRealTitle(title) {
			title = PlaceholderTitle(p.Agent)
		}
	}
	rootGoal := p.Prompt
	if rootGoal == "" {
		rootGoal = title
	}

	firstTurn := Turn{
		Index:     1,
		Prompt:    p.Prompt,
		Title:     title,
		Status:    "running",
		StartTime: nowMs,
		Detail:    p.Detail,
		LastHook:  p.Event,
		Timeline:  make([]TimelineItem, 0),
	}

	subCount := 0
	if p.Event == "subagentStart" {
		subCount = 1
	}

	task := &Task{
		ID:             p.ID,
		ParentID:       p.ParentID,
		SubagentCount:  subCount,
		Agent:          p.Agent,
		Repo:           p.Repo,
		Branch:         p.Branch,
		RootGoal:       rootGoal,
		Title:          title,
		Prompt:         p.Prompt,
		Status:         "running",
		StartTime:      nowMs,
		ActiveRunStart: nowMs,
		ActiveRunIndex: 1,
		TotalRuns:      1,
		Runs:           []Turn{firstTurn},
		LastHook:       p.Event,
		Detail:         p.Detail,
		PID:            p.PID,
		PGID:           p.PGID,
		KeyID:          p.KeyID,
	}

	return task
}

// RequestAbort 标记当前会话请求中断。
func (t *Task) RequestAbort(reason string, nowMs int64, nowStr string) {
	if t.Status == "completed" || t.Status == "failed" {
		return
	}
	t.ControlState = "abort_requested"
	t.Version++
	if reason == "" {
		reason = "用户从 Web 看板请求中断会话"
	}
	t.AbortReason = reason
	t.Detail = "用户请求中断中..."
	if len(t.Runs) > 0 {
		curRun := &t.Runs[len(t.Runs)-1]
		curRun.Detail = "用户请求中断中..."
		curRun.Timeline = append(curRun.Timeline, TimelineItem{
			Time:  nowStr,
			Event: "abortRequested",
			Desc:  reason,
		})
	}
}

// MarkAborted 当拦截器成功阻断 Agent 或收到终止收口时，将任务标记为中断终态。
func (t *Task) MarkAborted(reason string, nowMs int64, nowStr string) {
	t.ControlState = "aborted"
	t.Version++
	if reason == "" {
		reason = "会话已被用户成功中断"
	}
	t.AbortReason = reason
	if len(t.Runs) > 0 {
		curRun := &t.Runs[len(t.Runs)-1]
		curRun.closeAs("failed", nowMs)
		curRun.Detail = reason
		curRun.Timeline = append(curRun.Timeline, TimelineItem{
			Time:  nowStr,
			Event: "aborted",
			Desc:  reason,
		})
	}
	t.Status = "failed"
	t.EndTime = nowMs
	t.Detail = reason
	t.recountLifetime()
}

// MarkKilled 标记当前会话已被进程级强杀。
func (t *Task) MarkKilled(reason string, nowMs int64, nowStr string) {
	t.ControlState = "killed"
	t.Version++
	if reason == "" {
		reason = "会话已被用户强制强杀 (SIGTERM/SIGKILL)"
	}
	t.AbortReason = reason
	t.Status = "failed"
	t.EndTime = nowMs
	t.Detail = reason
	if len(t.Runs) > 0 {
		curRun := &t.Runs[len(t.Runs)-1]
		curRun.closeAs("failed", nowMs)
		curRun.Detail = reason
		curRun.Timeline = append(curRun.Timeline, TimelineItem{
			Time:  nowStr,
			Event: "killed",
			Desc:  reason,
		})
	}
	t.recountLifetime()
}

// IsAbortRequested 检查是否处于请求中断状态。
func (t *Task) IsAbortRequested() bool {
	return t.ControlState == "abort_requested"
}

// RecordActionDenial 记录一次动作被拦截/拒绝，追加时间线并保持中断中状态
func (t *Task) RecordActionDenial(reason string, nowMs int64, nowStr string) {
	t.Version++
	if reason == "" {
		reason = "操作已被拦截/拒绝"
	}
	t.Detail = reason
	if len(t.Runs) > 0 {
		curRun := &t.Runs[len(t.Runs)-1]
		curRun.Detail = reason
		curRun.Timeline = append(curRun.Timeline, TimelineItem{
			Time:  nowStr,
			Event: "actionDenied",
			Desc:  reason,
		})
	}
}

// RecordContextInjected 记录一次动态上下文注入，自增聚合根版本号并追加至当前 Run 时间线
func (t *Task) RecordContextInjected(content string, nowStr string) {
	t.Version++
	if len(t.Runs) > 0 {
		curRun := &t.Runs[len(t.Runs)-1]
		curRun.Timeline = append(curRun.Timeline, TimelineItem{
			Time:  nowStr,
			Event: "contextInjected",
			Desc:  fmt.Sprintf("动态注入上下文: %s", content),
		})
	}
}

// IsStartHook 判断事件是否为一轮对话的开端（新 Prompt / 会话启动）。
func IsStartHook(event string) bool {
	switch event {
	case "sessionStart", "onStart", "beforeSubmitPrompt", "UserPromptSubmit", "SessionStart":
		return true
	default:
		return false
	}
}

// IsTerminalHook 判断事件是否为会话/轮次收口（完成、失败或 Cursor 回复交付）。
func IsTerminalHook(event string) bool {
	switch event {
	case "agentCompletion", "onComplete", "complete", "Stop", "stop",
		"SessionEnd", "sessionEnd", "afterAgentResponse", "failed", "error":
		return true
	default:
		return false
	}
}

// IsLifecycleShellEvent 判断是否仅为会话开闭壳事件，不含工具调用或其它实际工作。
func IsLifecycleShellEvent(event string) bool {
	return IsStartHook(event) || IsTerminalHook(event)
}

// IsVacuousLifecycle 判断上报是否为空开会话的开闭事件：无 Prompt、无真实标题、无 AI 回复。
// Cursor 打开 Agent 后立刻关闭会连打 sessionStart + sessionEnd，这类事件不应落成看板卡片。
func IsVacuousLifecycle(p EventPayload) bool {
	if strings.TrimSpace(p.Prompt) != "" {
		return false
	}
	if IsRealTitle(p.Title) {
		return false
	}
	if strings.TrimSpace(p.AIResponse) != "" {
		return false
	}
	return IsLifecycleShellEvent(p.Event)
}

// HasUserWork 判断会话是否产生过真实工作（Prompt、工具、AI 回复或人工中断）。
func (t *Task) HasUserWork() bool {
	if t == nil {
		return false
	}
	if strings.TrimSpace(t.Prompt) != "" || strings.TrimSpace(t.AbortReason) != "" || t.ControlState != "" {
		return true
	}
	if IsRealTitle(t.Title) {
		return true
	}
	for _, run := range t.Runs {
		if strings.TrimSpace(run.Prompt) != "" || strings.TrimSpace(run.AIResponse) != "" {
			return true
		}
		if IsRealTitle(run.Title) {
			return true
		}
		for _, item := range run.Timeline {
			if !IsLifecycleShellEvent(item.Event) {
				return true
			}
		}
	}
	return false
}

// ShouldStartNewTurn 判断是否应该为当前 Task 开启新的一轮（Run）。
func (t *Task) ShouldStartNewTurn(p EventPayload) bool {
	if len(t.Runs) == 0 {
		return true
	}
	curRun := &t.Runs[len(t.Runs)-1]

	// 工具 / 完成类事件绝不能靠虚高的 turn_index 拆出新轮，否则会出现
	// 「中间一轮卡在 running、后续轮次已 completed」的空洞矩阵。
	if !IsStartHook(p.Event) {
		return false
	}
	if curRun.Status == "completed" || curRun.Status == "failed" {
		return true
	}

	// 首轮刚创建（NewTask 后立刻 ApplyEvent 同一条事件，或 sessionStart 后第一条 Prompt）
	// 仍属于填充当前轮，不能被虚高 turn_index 拆开。
	if t.isInitializingFirstRun(curRun, p) {
		return false
	}

	if p.TurnIndex > t.TotalRuns {
		return true
	}
	// stop 丢失时 turn_index 可能仍不准，但新 Prompt 正文已变，视为用户开启了下一轮。
	if p.Prompt != "" && curRun.Prompt != "" && p.Prompt != curRun.Prompt {
		return true
	}
	return false
}

func (t *Task) isInitializingFirstRun(curRun *Turn, p EventPayload) bool {
	if t.TotalRuns != 1 || curRun == nil {
		return false
	}
	if len(curRun.Timeline) > 0 {
		return false
	}
	return curRun.Prompt == "" || p.Prompt == "" || curRun.Prompt == p.Prompt
}

// closeAs 将仍在执行的一轮收口为终态，并写入耗时。已结束的轮次不会被覆盖。
func (r *Turn) closeAs(status string, nowMs int64) {
	if r == nil {
		return
	}
	if r.Status == "failed" {
		return
	}
	if r.Status == "completed" && status != "failed" {
		return
	}
	r.Status = status
	r.EndTime = nowMs
	diffSec := (r.EndTime - r.StartTime) / 1000
	if diffSec < 0 {
		diffSec = 0
	}
	r.Duration = FormatDuration(diffSec)
}

func (t *Task) recountLifetime() {
	var totalSec int64
	for _, r := range t.Runs {
		if r.EndTime > r.StartTime {
			totalSec += (r.EndTime - r.StartTime) / 1000
		}
	}
	t.TotalLifetime = totalSec
	t.Duration = FormatDuration(totalSec)
}

// CloseOrphanRuns 将非最后一轮仍停在 running 的空洞收口。
// 用于「上一轮 stop 丢失后又开了新轮」以及从磁盘恢复历史会话。
func (t *Task) CloseOrphanRuns(nowMs int64, nowStr string) bool {
	if t == nil || len(t.Runs) < 2 {
		return false
	}
	if nowStr == "" {
		nowStr = time.Unix(nowMs/1000, 0).Format("15:04:05")
	}
	changed := false
	last := len(t.Runs) - 1
	for i := 0; i < last; i++ {
		run := &t.Runs[i]
		if run.Status != "running" && run.Status != "" {
			continue
		}
		endMs := nowMs
		if nextStart := t.Runs[i+1].StartTime; nextStart > 0 {
			endMs = nextStart
		}
		run.closeAs("completed", endMs)
		run.Timeline = append(run.Timeline, TimelineItem{
			Time:  nowStr,
			Event: "runSuperseded",
			Desc:  "下一轮已开始，本轮自动收口",
		})
		changed = true
	}
	if changed {
		t.recountLifetime()
	}
	return changed
}

// StartNewTurn 为会话开启新的一轮执行周期。若上一轮仍在 running，先将其收口，避免矩阵出现空洞。
func (t *Task) StartNewTurn(p EventPayload, nowMs int64, nowStr string) {
	if len(t.Runs) > 0 {
		prev := &t.Runs[len(t.Runs)-1]
		if prev.Status == "running" || prev.Status == "" {
			prev.closeAs("completed", nowMs)
			if nowStr == "" {
				nowStr = time.Unix(nowMs/1000, 0).Format("15:04:05")
			}
			prev.Timeline = append(prev.Timeline, TimelineItem{
				Time:  nowStr,
				Event: "runSuperseded",
				Desc:  "下一轮已开始，本轮自动收口",
			})
			t.recountLifetime()
		}
	}

	newIdx := t.TotalRuns + 1
	newTitle := p.Title
	if !IsRealTitle(newTitle) {
		if p.Prompt != "" {
			newTitle = CleanPromptTitle(p.Prompt)
		}
		if !IsRealTitle(newTitle) {
			newTitle = fmt.Sprintf("Run #%d", newIdx)
		}
	}

	newTurn := Turn{
		Index:     newIdx,
		Prompt:    p.Prompt,
		Title:     newTitle,
		Status:    "running",
		StartTime: nowMs,
		Detail:    p.Detail,
		LastHook:  p.Event,
		Timeline:  make([]TimelineItem, 0),
	}

	t.Runs = append(t.Runs, newTurn)
	t.TotalRuns = newIdx
	t.ActiveRunIndex = newIdx
	t.ActiveRunStart = nowMs
	t.Status = "running"
	t.refreshHeuristicTitle(newTitle)
}

// ApplyEvent 将 Hook 上报事件应用到当前 Task 聚合根并更新状态机。
func (t *Task) ApplyEvent(p EventPayload, nowMs int64, nowStr string) {
	t.CloseOrphanRuns(nowMs, nowStr)
	if t.ShouldStartNewTurn(p) {
		t.StartNewTurn(p, nowMs, nowStr)
	}

	curRunIdx := len(t.Runs) - 1
	curRun := &t.Runs[curRunIdx]

	curRun.LastHook = p.Event
	curRun.Detail = p.Detail
	if p.AIResponse != "" {
		curRun.AIResponse = p.AIResponse
	}
	if curRun.Prompt == "" && p.Prompt != "" {
		curRun.Prompt = p.Prompt
	}
	if IsRealTitle(p.Title) {
		curRun.Title = p.Title
	} else if (IsPlaceholderTitle(curRun.Title) || curRun.Title == "") && p.Prompt != "" {
		curRun.Title = CleanPromptTitle(p.Prompt)
	}

	// RootGoal 仅在仍为占位时写入首轮 Prompt 原文，后续轮次与 LLM 均不得改写。
	if IsPlaceholderTitle(t.RootGoal) || t.RootGoal == "" || t.RootGoal == PlaceholderTitle(t.Agent) {
		if p.Prompt != "" {
			t.RootGoal = p.Prompt
		} else if IsRealTitle(p.Title) {
			t.RootGoal = p.Title
		}
	}
	heuristic := p.Title
	if !IsRealTitle(heuristic) && p.Prompt != "" {
		heuristic = CleanPromptTitle(p.Prompt)
	}
	t.refreshHeuristicTitle(heuristic)
	if t.Prompt == "" && p.Prompt != "" {
		t.Prompt = p.Prompt
	}

	if p.ParentID != "" && t.ParentID == "" && (p.Event == "sessionStart" || p.Event == "beforeSubmitPrompt" || p.Event == "UserPromptSubmit") {
		t.ParentID = p.ParentID
	}

	// 时间线防抖去重：连续相同说明不追加。
	// Cursor 会在同一秒连打 afterAgentResponse 与 stop（映射为 agentCompletion），两边 desc 都是同一句「AI 回复」。
	shouldAppend := true
	if len(curRun.Timeline) > 0 {
		last := curRun.Timeline[len(curRun.Timeline)-1]
		if last.Desc == p.Detail {
			shouldAppend = false
		}
	}
	if shouldAppend {
		// 仅在真实向外派发非重复子代理时递增（支持多级嵌套子代理）
		if p.Event == "subagentStart" {
			t.SubagentCount++
		}
		curRun.Timeline = append(curRun.Timeline, TimelineItem{
			Time:         nowStr,
			Event:        p.Event,
			Desc:         p.Detail,
			SubagentType: p.SubagentType,
			SubagentID:   p.SubagentID,
		})
	}

	// 状态流转判定
	switch p.Event {
	case "sessionStart", "onStart", "beforeSubmitPrompt", "UserPromptSubmit", "SessionStart":
		curRun.Status = "running"
		t.Status = "running"
		t.ActiveRunStart = curRun.StartTime
	case "agentCompletion", "onComplete", "complete", "Stop", "stop", "SessionEnd", "sessionEnd", "afterAgentResponse":
		// afterAgentResponse：Cursor 回复已对用户交付。stop 晚到或丢失时作为本轮兜底收口。
		if t.IsAbortRequested() {
			// 如果处于中断请求中，真正的终端收口事件到来时，才正式标记为 aborted 终态
			t.MarkAborted(t.AbortReason, nowMs, nowStr)
		} else if t.Status != "failed" {
			curRun.closeAs("completed", nowMs)
			t.Status = "completed"
			t.EndTime = nowMs
			t.recountLifetime()
		}
	case "failed", "error":
		// 会话级中断/崩溃（Cursor stop 的 aborted/error 由上报器映射为 failed）
		curRun.closeAs("failed", nowMs)
		t.Status = "failed"
		t.EndTime = nowMs
		t.recountLifetime()
	case "toolFailure", "PostToolUseFailure", "postToolUseFailure":
		// 单个工具执行异常（如 bash 非零退出），非致命中断，任务与 Run 保持 running
		if curRun.Status == "" {
			curRun.Status = "running"
		}
		if t.Status == "" {
			t.Status = "running"
		}
	}

	t.LastHook = p.Event
	t.Detail = p.Detail
	if p.PID > 0 {
		t.PID = p.PID
	}
	if p.PGID > 0 {
		t.PGID = p.PGID
	}
	if p.KeyID != "" {
		t.KeyID = p.KeyID
	}
	t.Version++
	t.Turns = t.Runs             // 兼容 turns 别名
	t.Timeline = curRun.Timeline // 兼容顶层 timeline
}

// Clone 返回当前 Task 聚合根的独立深拷贝副本，确保并发安全。
func (t *Task) Clone() *Task {
	if t == nil {
		return nil
	}
	cp := *t

	if t.Runs != nil {
		cp.Runs = make([]Turn, len(t.Runs))
		for i, run := range t.Runs {
			runCp := run
			if run.Timeline != nil {
				runCp.Timeline = make([]TimelineItem, len(run.Timeline))
				copy(runCp.Timeline, run.Timeline)
			}
			cp.Runs[i] = runCp
		}
		cp.Turns = cp.Runs
	}

	if t.Timeline != nil {
		cp.Timeline = make([]TimelineItem, len(t.Timeline))
		copy(cp.Timeline, t.Timeline)
	}

	return &cp
}
