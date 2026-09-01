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

// HandleTasks 处理任务查询与清空已完成。
func (h *Handler) HandleTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodDelete {
		_ = h.svc.ClearFinishedTasks()
		w.Write([]byte(`{"cleared":true}`))
		return
	}

	tasks := h.svc.GetAllTasks()
	json.NewEncoder(w).Encode(tasks)
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
