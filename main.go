// AGENT MONITOR：接收 Cursor Agent hook 上报，通过 SSE 推送到 Monitor 前端。
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

// Task 表示 Monitor 上的一个 Agent 任务。
type Task struct {
    ID        string         `json:"id"`                 // 会话/任务 ID
    Agent     string         `json:"agent"`              // Agent 名称
    Repo      string         `json:"repo"`               // 仓库名
    Branch    string         `json:"branch"`             // 当前分支
    Event     string         `json:"event"`              // 最近一次事件名
    Title     string         `json:"title"`              // 任务标题（卡片短标题）
    Prompt    string         `json:"prompt,omitempty"`   // 首条用户 Prompt，抽屉展示
    Status    string         `json:"status"`             // running / completed / failed
    StartTime int64          `json:"startTime"`          // 开始时间，Unix 毫秒
    EndTime   int64          `json:"endTime,omitempty"`  // 结束时间，Unix 毫秒
    Duration  string         `json:"duration,omitempty"` // 耗时，如 01m 23s
    LastHook  string         `json:"lastHook"`           // 最近一次 hook 事件
    Detail    string         `json:"detail"`             // 当前操作详情
    Timeline  []TimelineItem `json:"timeline"`           // 事件时间轴
}

// EventPayload 是 /api/event 接收的 hook 上报结构。
type EventPayload struct {
    ID        string `json:"id"`        // 会话/任务 ID，空则自动生成
    Agent     string `json:"agent"`     // Agent 名称
    Repo      string `json:"repo"`      // 仓库信息
    Branch    string `json:"branch"`    // 分支名
    Event     string `json:"event"`     // hook 事件名，决定任务状态流转
    Title     string `json:"title"`     // 任务标题；占位标题可被后续真实 Prompt 覆盖
    Prompt    string `json:"prompt"`    // 完整首条 Prompt，空则不更新
    Timestamp int64  `json:"timestamp"` // Unix 秒；为 0 则用服务端当前时间
    Detail    string `json:"detail"`    // 本次操作的简要说明
}

// Hub 负责任务存储与 SSE 客户端广播。
type Hub struct {
    mu        sync.RWMutex         // 保护 tasks 与 clients
    tasks     map[string]*Task     // 任务列表，key 为会话/任务 ID
    clients   map[chan string]bool // 已连接的 SSE 客户端
    addClient chan chan string     // 注册新客户端
    rmClient  chan chan string     // 移除客户端
    broadcast chan string          // 广播消息通道
}

// newHub 创建空的任务中心，broadcast 带缓冲以免上报被阻塞。
func newHub() *Hub {
    return &Hub{
        tasks:     make(map[string]*Task),
        clients:   make(map[chan string]bool),
        addClient: make(chan chan string),
        rmClient:  make(chan chan string),
        broadcast: make(chan string, 100),
    }
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
    m := sec / 60
    s := sec % 60
    return fmt.Sprintf("%02dm %02ds", m, s)
}

// isPlaceholderTitle 判断标题是否为可被真实 Prompt 覆盖的占位文案。
func isPlaceholderTitle(title string) bool {
    t := strings.TrimSpace(title)
    if t == "" || t == "未命名" || t == "未命名任务" {
        return true
    }
    if strings.HasPrefix(t, "CLI Task") {
        return true
    }
    // 「Cursor Agent 任务」「AI Agent 任务」等
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

// handleEvent 接收 Hook 上报：创建或更新任务，并广播给所有 Monitor 客户端。
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

    // 补齐缺省字段，保证后续能稳定按 ID 聚合同一会话
    if p.ID == "" {
        p.ID = fmt.Sprintf("task-%d", time.Now().UnixNano()%10000)
    }
    if p.Agent == "" {
        p.Agent = "AI Agent"
    }
    if p.Timestamp == 0 {
        p.Timestamp = time.Now().Unix()
    }

    nowStr := time.Unix(p.Timestamp, 0).Format("15:04:05")

    h.mu.Lock()
    task, exists := h.tasks[p.ID]
    if !exists {
        // 首次上报：新建任务，默认进入 running
        title := p.Title
        if !isRealTitle(title) {
            title = placeholderTitle(p.Agent)
        }
        task = &Task{
            ID:        p.ID,
            Agent:     p.Agent,
            Repo:      p.Repo,
            Branch:    p.Branch,
            Title:     title,
            Prompt:    p.Prompt,
            Status:    "running",
            StartTime: p.Timestamp * 1000,
            Timeline:  make([]TimelineItem, 0),
        }
        h.tasks[p.ID] = task
    }

    task.LastHook = p.Event
    task.Detail = p.Detail
    // 占位标题允许被后续更明确的 Prompt 覆盖；已有真实标题不再改
    if isRealTitle(p.Title) && isPlaceholderTitle(task.Title) {
        task.Title = p.Title
    }
    // 完整 Prompt 只保留首条，避免后续跟进消息覆盖任务描述
    if task.Prompt == "" && p.Prompt != "" {
        task.Prompt = p.Prompt
    }

    task.Timeline = append(task.Timeline, TimelineItem{
        Time:  nowStr,
        Event: p.Event,
        Desc:  p.Detail,
    })

    // 按 Hook 事件名映射 Monitor 列：启动→运行中，完成→已完成，失败/中断→异常
    switch p.Event {
    case "sessionStart", "onStart":
        task.Status = "running"
    case "agentCompletion", "onComplete", "complete":
        task.Status = "completed"
        task.EndTime = p.Timestamp * 1000
        diffSec := p.Timestamp - (task.StartTime / 1000)
        if diffSec < 0 {
            diffSec = 0
        }
        task.Duration = formatDuration(diffSec)
    case "stop", "failed", "error":
        task.Status = "failed"
        task.EndTime = p.Timestamp * 1000
    }

    taskJSON, _ := json.Marshal(task)
    h.mu.Unlock()

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

    // 新连接先补齐快照，避免 Monitor 空白
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
        case <-notify: // 客户端断开
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
        h.mu.Lock()
        for id, t := range h.tasks {
            // 只清已结束的卡片，进行中的任务保留
            if t.Status == "completed" || t.Status == "failed" {
                delete(h.tasks, id)
            }
        }
        h.mu.Unlock()
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
    port := os.Getenv("PORT") // 可通过 PORT 覆盖默认端口
    if port == "" {
        port = "8000"
    }

    hub := newHub()
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
