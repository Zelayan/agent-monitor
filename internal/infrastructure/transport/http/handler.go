package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"agent-monitor/internal/application/monitor"
	"agent-monitor/internal/domain/task"
)

// Handler 提供 Monitor 的 HTTP API 与前端页面路由处理。
type Handler struct {
	svc        *monitor.MonitorService
	hub        *monitor.Hub
	staticHTML []byte
}

// NewHandler 创建 HTTP 处理器实例。
func NewHandler(svc *monitor.MonitorService, hub *monitor.Hub, staticHTML []byte) *Handler {
	return &Handler{
		svc:        svc,
		hub:        hub,
		staticHTML: staticHTML,
	}
}

// RegisterRoutes 在给定的 ServeMux 上注册路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/event", h.HandleEvent)
	mux.HandleFunc("/api/stream", h.HandleStream)
	mux.HandleFunc("/api/tasks", h.HandleTasks)
	mux.HandleFunc("/", h.HandleIndex)
}

// HandleEvent 处理 Hook POST 上报。
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p task.EventPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := h.svc.HandleHookEvent(p); err != nil {
		http.Error(w, "Internal server error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// HandleStream 提供 SSE 长连接流。
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := h.hub.Subscribe()
	defer h.hub.Unsubscribe(clientChan)

	// 新连接先发送所有当前任务快照
	tasks := h.svc.GetAllTasks()
	for _, t := range tasks {
		data, _ := json.Marshal(t)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
	}
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

// HandleTasks 处理任务查询与多种模式删除（全部/选中/已完成）。
func (h *Handler) HandleTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodDelete {
		var req monitor.DeleteTasksRequest
		// 1. 支持通过 URL Query ?all=true 清空全部
		if r.URL.Query().Get("all") == "true" || r.URL.Query().Get("all") == "1" {
			req.All = true
		}
		// 2. 支持通过 JSON Body 传入指定要删除的 ids 列表
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		// 3. 支持通过 URL Query ?ids=id1,id2
		if idsQuery := r.URL.Query().Get("ids"); idsQuery != "" && len(req.IDs) == 0 {
			req.IDs = splitAndTrim(idsQuery, ",")
		}

		deleted := h.svc.DeleteTasks(req)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cleared":    true,
			"deletedIds": deleted,
			"count":      len(deleted),
		})
		return
	}

	tasks := h.svc.GetAllTasks()
	json.NewEncoder(w).Encode(tasks)
}

func splitAndTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, p := range json.RawMessage(s) { // fallback
		_ = p
	}
	for _, raw := range []string{s} {
		for _, part := range json.RawMessage(raw) {
			_ = part
		}
	}
	// standard split
	for _, token := range stringsSplit(s, sep) {
		token = trim(token)
		if token != "" {
			parts = append(parts, token)
		}
	}
	return parts
}

func stringsSplit(s, sep string) []string {
	var res []string
	start := 0
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			res = append(res, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	res = append(res, s[start:])
	return res
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// HandleIndex 渲染嵌入的前端 SPA 页面。
func (h *Handler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(h.staticHTML)
}
