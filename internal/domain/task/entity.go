package task

import (
	"fmt"
	"time"
)

// TimelineItem 记录任务时间轴上的单条事件（值对象）。
type TimelineItem struct {
	Time  string `json:"time"`  // 事件时间，格式 HH:MM:SS
	Event string `json:"event"` // hook 事件名
	Desc  string `json:"desc"`  // 事件描述
}

// Turn 表示单个会话内的独立一轮执行周期（Run 实体）。
type Turn struct {
	Index     int            `json:"index"`               // 轮次序号 1, 2, 3...
	Prompt    string         `json:"prompt,omitempty"`   // 当轮 Prompt 正文
	Title     string         `json:"title,omitempty"`    // 当轮标题
	Status    string         `json:"status"`             // running / completed / failed
	StartTime int64          `json:"startTime"`          // 当轮开始时间，Unix 毫秒
	EndTime   int64          `json:"endTime,omitempty"`  // 当轮结束时间，Unix 毫秒
	Duration  string         `json:"duration,omitempty"` // 当轮实际执行耗时
	Detail    string         `json:"detail,omitempty"`   // 当轮最新操作描述
	LastHook  string         `json:"lastHook,omitempty"` // 当轮最后一次 Hook 事件
	Timeline  []TimelineItem `json:"timeline"`           // 当轮独立 Hook 轨迹
}

// Task 表示 Monitor 上的一个 Agent 会话容器（聚合根 Workflow）。
type Task struct {
	ID             string         `json:"id"`                       // 会话 ID (如 sess_xxx)
	Agent          string         `json:"agent"`                    // Agent 名称
	Repo           string         `json:"repo"`                     // 仓库名与分支
	Branch         string         `json:"branch"`                   // 分支名
	Event          string         `json:"event"`                    // 最近一次事件名
	RootGoal       string         `json:"rootGoal"`                 // 会话核心总目标（首轮 Prompt）
	Title          string         `json:"title"`                    // 主标题（兼容字段）
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
}

// EventPayload 是 Hook 上报的数据传输对象 (DTO)。
type EventPayload struct {
	ID        string `json:"id"`                   // 会话/任务 ID，空则自动生成
	Agent     string `json:"agent"`                // Agent 名称
	Repo      string `json:"repo"`                 // 仓库信息
	Branch    string `json:"branch"`               // 分支名
	Event     string `json:"event"`                // hook 事件名，决定任务状态流转
	Title     string `json:"title"`                // 任务标题
	Prompt    string `json:"prompt"`               // 本轮 Prompt
	Timestamp int64  `json:"timestamp"`            // Unix 秒；为 0 则用服务端当前时间
	Detail    string `json:"detail"`               // 本次操作的简要说明
	TurnIndex int    `json:"turn_index,omitempty"` // 上报指定的轮次（可选）
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

	task := &Task{
		ID:             p.ID,
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
	}

	return task
}

// ShouldStartNewTurn 判断是否应该为当前 Task 开启新的一轮（Run）。
func (t *Task) ShouldStartNewTurn(p EventPayload) bool {
	if len(t.Runs) == 0 {
		return true
	}
	curRun := &t.Runs[len(t.Runs)-1]
	isStartEvent := (p.Event == "sessionStart" || p.Event == "beforeSubmitPrompt" || p.Event == "UserPromptSubmit" || p.Event == "SessionStart")

	if isStartEvent && (curRun.Status == "completed" || curRun.Status == "failed") {
		return true
	}
	if p.TurnIndex > t.TotalRuns {
		return true
	}
	return false
}

// StartNewTurn 为会话开启新的一轮执行周期。
func (t *Task) StartNewTurn(p EventPayload, nowMs int64) {
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
}

// ApplyEvent 将 Hook 上报事件应用到当前 Task 聚合根并更新状态机。
func (t *Task) ApplyEvent(p EventPayload, nowMs int64, nowStr string) {
	if t.ShouldStartNewTurn(p) {
		t.StartNewTurn(p, nowMs)
	}

	curRunIdx := len(t.Runs) - 1
	curRun := &t.Runs[curRunIdx]

	curRun.LastHook = p.Event
	curRun.Detail = p.Detail
	if curRun.Prompt == "" && p.Prompt != "" {
		curRun.Prompt = p.Prompt
	}
	if IsRealTitle(p.Title) {
		curRun.Title = p.Title
	} else if (IsPlaceholderTitle(curRun.Title) || curRun.Title == "") && p.Prompt != "" {
		curRun.Title = CleanPromptTitle(p.Prompt)
	}

	// 动态覆写 Task 容器级别的 RootGoal、Title 与 Prompt（当初始创建时为占位符时）
	if IsPlaceholderTitle(t.RootGoal) || t.RootGoal == "" || t.RootGoal == PlaceholderTitle(t.Agent) {
		if p.Prompt != "" {
			t.RootGoal = p.Prompt
		} else if IsRealTitle(p.Title) {
			t.RootGoal = p.Title
		}
	}
	if IsPlaceholderTitle(t.Title) || t.Title == "" {
		if IsRealTitle(p.Title) {
			t.Title = p.Title
		} else if p.Prompt != "" {
			t.Title = CleanPromptTitle(p.Prompt)
		}
	}
	if t.Prompt == "" && p.Prompt != "" {
		t.Prompt = p.Prompt
	}

	curRun.Timeline = append(curRun.Timeline, TimelineItem{
		Time:  nowStr,
		Event: p.Event,
		Desc:  p.Detail,
	})

	// 状态流转判定
	switch p.Event {
	case "sessionStart", "onStart", "beforeSubmitPrompt", "UserPromptSubmit", "SessionStart":
		curRun.Status = "running"
		t.Status = "running"
		t.ActiveRunStart = curRun.StartTime
	case "agentCompletion", "onComplete", "complete", "Stop", "SessionEnd":
		curRun.Status = "completed"
		curRun.EndTime = nowMs
		diffSec := (curRun.EndTime - curRun.StartTime) / 1000
		if diffSec < 0 {
			diffSec = 0
		}
		curRun.Duration = FormatDuration(diffSec)

		t.Status = "completed"
		t.EndTime = nowMs

		// 重新计算全生命周期总执行秒数
		var totalSec int64 = 0
		for _, r := range t.Runs {
			if r.EndTime > r.StartTime {
				totalSec += (r.EndTime - r.StartTime) / 1000
			}
		}
		t.TotalLifetime = totalSec
		t.Duration = FormatDuration(totalSec)
	case "stop", "failed", "error", "PostToolUseFailure":
		curRun.Status = "failed"
		curRun.EndTime = nowMs
		t.Status = "failed"
		t.EndTime = nowMs
	}

	t.LastHook = p.Event
	t.Detail = p.Detail
	t.Turns = t.Runs                // 兼容 turns 别名
	t.Timeline = curRun.Timeline    // 兼容顶层 timeline
}
