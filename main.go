// AGENT MONITOR：接收 AI Agent (ZCode/Cursor/Claude) Hook 上报，通过 SSE 推送到 Monitor 前端。
// 支持企业级 Session Workflow & Multi-Run Engine（单会话支持 50+ / 100+ 轮次独立生命周期追踪）。
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed static/index.html
var indexHTML []byte // 将 Monitor 页面嵌入二进制，无需额外静态文件

// TimelineItem 记录任务时间轴上的单条事件。
type TimelineItem struct {
	Time  string `json:"time"`  // 事件时间，格式 HH:MM:SS
	Event string `json:"event"` // hook 事件名
	Desc  string `json:"desc"`  // 事件描述
}

// Turn 表示单个会话内的独立一轮执行周期（Run）。
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

// Task 表示 Monitor 上的一个 Agent 会话容器（Workflow）。
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

// EventPayload 是 /api/event 接收的 hook 上报结构。
type EventPayload struct {
	ID        string `json:"id"`                  // 会话/任务 ID，空则自动生成
	Agent     string `json:"agent"`               // Agent 名称
	Repo      string `json:"repo"`                // 仓库信息
	Branch    string `json:"branch"`              // 分支名
	Event     string `json:"event"`               // hook 事件名，决定任务状态流转
	Title     string `json:"title"`               // 任务标题
	Prompt    string `json:"prompt"`              // 本轮 Prompt
	Timestamp int64  `json:"timestamp"`           // Unix 秒；为 0 则用服务端当前时间
	Detail    string `json:"detail"`              // 本次操作的简要说明
	TurnIndex int    `json:"turn_index,omitempty"`// 上报指定的轮次（可选）
}

// Hub 负责任务存储与 SSE 客户端广播。
type Hub struct {
	mu        sync.RWMutex         // 保护 tasks 与 clients
	tasks     map[string]*Task     // 任务列表，key 为会话/任务 ID
	store     Store                // 抽象存储层接口
	clients   map[chan string]bool // 已连接的 SSE 客户端
	addClient chan chan string     // 注册新客户端
	rmClient  chan chan string     // 移除客户端
	broadcast chan string          // 广播消息通道
}

// newHub 创建任务中心并从 Store 加载已有会话，broadcast 带缓冲以免上报被阻塞。
func newHub(store Store) *Hub {
	h := &Hub{
		tasks:     make(map[string]*Task),
		store:     store,
		clients:   make(map[chan string]bool),
		addClient: make(chan chan string),
		rmClient:  make(chan chan string),
		broadcast: make(chan string, 100),
	}

	if store != nil {
		persistedTasks, err := store.LoadAll()
		if err != nil {
			log.Printf("[Store] Warning: failed to load persisted tasks: %v", err)
		} else {
			for _, t := range persistedTasks {
				if t != nil && t.ID != "" {
					h.tasks[t.ID] = t
				}
			}
			if len(h.tasks) > 0 {
				log.Printf("[Store] Loaded %d persisted sessions from storage", len(h.tasks))
			}
		}
	}

	return h
}

// run 在独立 goroutine 中处理客户端注册/注销与消息广播。
func (h *Hub) run() {
	for {
		select {
		case ch := <-h.addClient:
			h.mu.Lock()
			h.clients[ch] = true
			h.mu.Unlock()
		case ch := <-h.rmClient:
			h.mu.Lock()
			delete(h.clients, ch)
			close(ch)
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			for ch := range h.clients {
				select {
				case ch <- msg: // 非阻塞发送，慢客户端直接跳过
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

// formatDuration 将秒数格式化为 "分m 秒s"。
func formatDuration(sec int64) string {
	if sec < 0 {
		sec = 0
	}
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02dm %02ds", m, s)
}

// isPlaceholderTitle 判断标题是否为可被真实 Prompt 覆盖的占位文案。
func isPlaceholderTitle(title string) bool {
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

func isRealTitle(title string) bool {
	return strings.TrimSpace(title) != "" && !isPlaceholderTitle(title)
}

func placeholderTitle(agent string) string {
	if strings.TrimSpace(agent) == "" {
		return "AI Agent 任务"
	}
	return agent + " 任务"
}

// cleanPromptTitle 从 Prompt 中提取首行作为简洁标题，过滤常用前缀标记并限制长度。
func cleanPromptTitle(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 移除常见标记前缀
		prefixes := []string{"#task", "#Task", "[board]", "[Board]", "任务:", "任务：", "TODO:", "todo:"}
		for _, p := range prefixes {
			line = strings.TrimPrefix(line, p)
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

// handleEvent 接收 Hook 上报：支持多轮会话（Runs）聚合与独立生命周期结算。
func (h *Hub) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p EventPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if p.ID == "" {
		p.ID = fmt.Sprintf("sess-%d", time.Now().UnixNano()%100000)
	}
	if p.Agent == "" {
		p.Agent = "AI Agent"
	}
	if p.Timestamp == 0 {
		p.Timestamp = time.Now().Unix()
	}

	nowMs := p.Timestamp * 1000
	nowStr := time.Unix(p.Timestamp, 0).Format("15:04:05")

	h.mu.Lock()
	task, exists := h.tasks[p.ID]
	if !exists {
		// 首次上报：创建会话容器，并初始化 Run #1
		title := p.Title
		if !isRealTitle(title) {
			if p.Prompt != "" {
				title = cleanPromptTitle(p.Prompt)
			}
			if !isRealTitle(title) {
				title = placeholderTitle(p.Agent)
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

		task = &Task{
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
		h.tasks[p.ID] = task
	} else {
		// 会话已存在：判断是否触发新 Run（上一轮已结束又收到 start/prompt，或者上报显式指定新 turn）
		isStartEvent := (p.Event == "sessionStart" || p.Event == "beforeSubmitPrompt" || p.Event == "UserPromptSubmit" || p.Event == "SessionStart")
		curRunIdx := len(task.Runs) - 1
		curRun := &task.Runs[curRunIdx]

		shouldStartNewTurn := false
		if isStartEvent && (curRun.Status == "completed" || curRun.Status == "failed") {
			shouldStartNewTurn = true
		} else if p.TurnIndex > task.TotalRuns {
			shouldStartNewTurn = true
		}

			if shouldStartNewTurn {
				// 开启全新 Run
				newIdx := task.TotalRuns + 1
				newTitle := p.Title
				if !isRealTitle(newTitle) {
					if p.Prompt != "" {
						newTitle = cleanPromptTitle(p.Prompt)
					}
					if !isRealTitle(newTitle) {
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
			task.Runs = append(task.Runs, newTurn)
			task.TotalRuns = newIdx
			task.ActiveRunIndex = newIdx
			task.ActiveRunStart = nowMs
			task.Status = "running"
		}
	}

	// 更新当前最新 Run 的状态与时间线
	curRunIdx := len(task.Runs) - 1
	curRun := &task.Runs[curRunIdx]

	curRun.LastHook = p.Event
	curRun.Detail = p.Detail
	if curRun.Prompt == "" && p.Prompt != "" {
		curRun.Prompt = p.Prompt
	}
	if isRealTitle(p.Title) {
		curRun.Title = p.Title
	} else if (isPlaceholderTitle(curRun.Title) || curRun.Title == "") && p.Prompt != "" {
		curRun.Title = cleanPromptTitle(p.Prompt)
	}

	// 动态覆写 Task 容器级别的 RootGoal、Title 与 Prompt（当初始创建时为占位符时）
	if isPlaceholderTitle(task.RootGoal) || task.RootGoal == "" || task.RootGoal == placeholderTitle(task.Agent) {
		if p.Prompt != "" {
			task.RootGoal = p.Prompt
		} else if isRealTitle(p.Title) {
			task.RootGoal = p.Title
		}
	}
	if isPlaceholderTitle(task.Title) || task.Title == "" {
		if isRealTitle(p.Title) {
			task.Title = p.Title
		} else if p.Prompt != "" {
			task.Title = cleanPromptTitle(p.Prompt)
		}
	}
	if task.Prompt == "" && p.Prompt != "" {
		task.Prompt = p.Prompt
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
		task.Status = "running"
		task.ActiveRunStart = curRun.StartTime
	case "agentCompletion", "onComplete", "complete", "Stop", "SessionEnd":
		curRun.Status = "completed"
		curRun.EndTime = nowMs
		diffSec := (curRun.EndTime - curRun.StartTime) / 1000
		if diffSec < 0 {
			diffSec = 0
		}
		curRun.Duration = formatDuration(diffSec)

		// 检查是否所有 Run 都处于完结状态
		task.Status = "completed"
		task.EndTime = nowMs

		// 重新计算全生命周期总执行秒数
		var totalSec int64 = 0
		for _, r := range task.Runs {
			if r.EndTime > r.StartTime {
				totalSec += (r.EndTime - r.StartTime) / 1000
			}
		}
		task.TotalLifetime = totalSec
		task.Duration = formatDuration(totalSec)
	case "stop", "failed", "error", "PostToolUseFailure":
		curRun.Status = "failed"
		curRun.EndTime = nowMs
		task.Status = "failed"
		task.EndTime = nowMs
	}

	task.LastHook = p.Event
	task.Detail = p.Detail
	task.Turns = task.Runs       // 兼容 turns 别名
	task.Timeline = curRun.Timeline // 兼容顶层 timeline

	taskJSON, _ := json.Marshal(task)
	h.mu.Unlock()

	// 异步持久化存储，不阻塞 Hook 上报与 SSE 推送
	if h.store != nil {
		go func(t *Task) {
			if err := h.store.SaveTask(t); err != nil {
				log.Printf("[Store] Error saving task %s: %v", t.ID, err)
			}
		}(task)
	}

	h.broadcast <- string(taskJSON)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleStream 提供 SSE 长连接：先推送当前全部任务，再持续推送增量更新。
func (h *Hub) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := make(chan string, 20)
	h.addClient <- clientChan
	defer func() {
		h.rmClient <- clientChan
	}()

	// 新连接先补齐快照
	h.mu.RLock()
	for _, t := range h.tasks {
		data, _ := json.Marshal(t)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
	}
	h.mu.RUnlock()
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// handleTasks GET 返回全部任务；DELETE 清除已完成和失败的任务。
func (h *Hub) handleTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodDelete {
		var toDelete []string
		h.mu.Lock()
		for id, t := range h.tasks {
			if t.Status == "completed" || t.Status == "failed" {
				delete(h.tasks, id)
				toDelete = append(toDelete, id)
			}
		}
		h.mu.Unlock()

		if h.store != nil && len(toDelete) > 0 {
			go func(ids []string) {
				for _, id := range ids {
					if err := h.store.DeleteTask(id); err != nil {
						log.Printf("[Store] Error deleting task file %s: %v", id, err)
					}
				}
			}(toDelete)
		}

		w.Write([]byte(`{"cleared":true}`))
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	list := make([]*Task, 0, len(h.tasks))
	for _, t := range h.tasks {
		list = append(list, t)
	}
	json.NewEncoder(w).Encode(list)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data/sessions"
	}

	store, err := NewJSONStore(dataDir)
	if err != nil {
		log.Printf("[Store] Warning: failed to initialize JSON store in %s: %v", dataDir, err)
	} else {
		defer store.Close()
	}

	hub := newHub(store)
	go hub.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/event", hub.handleEvent)   // Hook 上报入口
	mux.HandleFunc("/api/stream", hub.handleStream) // Monitor SSE 推送
	mux.HandleFunc("/api/tasks", hub.handleTasks)   // 任务列表 / 清空已完成
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	addr := ":" + port
	fmt.Printf("\nAGENT MONITOR running on http://127.0.0.1%s\n", addr)
	fmt.Printf("   Dashboard: http://127.0.0.1%s/\n\n", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
