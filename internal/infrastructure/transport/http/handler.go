package http

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Zelayan/agent-monitor/internal/application/monitor"
	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

// Handler 提供 Monitor 的 HTTP API 与前端页面路由处理。
type Handler struct {
	svc        *monitor.MonitorService
	hub        *monitor.Hub
	staticHTML []byte
	staticFS   fs.FS
	apiKey     string
}

// NewHandler 创建 HTTP 处理器实例。
func NewHandler(svc *monitor.MonitorService, hub *monitor.Hub, staticHTML []byte) *Handler {
	return &Handler{
		svc:        svc,
		hub:        hub,
		staticHTML: staticHTML,
	}
}

// WithAPIKey 设置访问 API Key，非空时对 /api/* 接口启用鉴权。
func (h *Handler) WithAPIKey(apiKey string) *Handler {
	h.apiKey = strings.TrimSpace(apiKey)
	return h
}

// WithStaticFS 设置静态资源文件系统（用于支持 /static/ 离线静态资源服务）。
func (h *Handler) WithStaticFS(staticFS fs.FS) *Handler {
	h.staticFS = staticFS
	return h
}

// RegisterRoutes 在给定的 ServeMux 上注册路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/event", h.HandleEvent)
	mux.HandleFunc("/api/stream", h.HandleStream)
	mux.HandleFunc("/api/tasks", h.HandleTasks)
	mux.HandleFunc("/manifest.json", h.HandleManifest)
	mux.HandleFunc("/sw.js", h.HandleServiceWorker)
	if h.staticFS != nil {
		mux.Handle("/static/", http.FileServer(http.FS(h.staticFS)))
	}
	mux.HandleFunc("/", h.HandleIndex)
}

func enableCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (h *Handler) checkAuth(r *http.Request) bool {
	if h.apiKey == "" {
		return true
	}

	// 1. Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(parts[1])), []byte(h.apiKey)) == 1 {
				return true
			}
		}
	}

	// 2. X-API-Key: <token>
	if xKey := r.Header.Get("X-API-Key"); xKey != "" {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(xKey)), []byte(h.apiKey)) == 1 {
			return true
		}
	}

	// 3. URL Query Parameter ?token=<token> 或 ?api_key=<token> (用于原生 EventSource SSE 长连接)
	if qToken := r.URL.Query().Get("token"); qToken != "" {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(qToken)), []byte(h.apiKey)) == 1 {
			return true
		}
	}
	if qKey := r.URL.Query().Get("api_key"); qKey != "" {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(qKey)), []byte(h.apiKey)) == 1 {
			return true
		}
	}

	return false
}

func (h *Handler) writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"Unauthorized: valid API key required"}`))
}

// HandleEvent 处理 Hook POST 上报。
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	if enableCORS(w, r) {
		return
	}

	if !h.checkAuth(r) {
		h.writeUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 限制请求体上限为 2MB，防止恶意/异常数据打崩内存
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

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
	if enableCORS(w, r) {
		return
	}

	if !h.checkAuth(r) {
		h.writeUnauthorized(w)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := h.hub.Subscribe()
	defer h.hub.Unsubscribe(clientChan)

	// 1. 新连接先发送所有当前任务快照
	tasks := h.svc.GetAllTasks()
	for _, t := range tasks {
		data, _ := json.Marshal(t)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
	}
	flusher.Flush()

	// 2. 15s 心跳 Ticker，防止云代理/反向代理（Nginx/Caddy/ALB）在无事件时空闲超时中断连接
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
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
	if enableCORS(w, r) {
		return
	}

	if !h.checkAuth(r) {
		h.writeUnauthorized(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodDelete {
		var req monitor.DeleteTasksRequest
		// 1. 支持通过 URL Query ?all=true 清空全部
		if r.URL.Query().Get("all") == "true" || r.URL.Query().Get("all") == "1" {
			req.All = true
		}
		// 2. 支持通过 JSON Body 传入指定要删除的 ids 列表
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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
	var parts []string
	for _, token := range strings.Split(s, sep) {
		token = strings.TrimSpace(token)
		if token != "" {
			parts = append(parts, token)
		}
	}
	return parts
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

// HandleManifest 返回 PWA Web App Manifest。
func (h *Handler) HandleManifest(w http.ResponseWriter, r *http.Request) {
	if h.staticFS != nil {
		data, err := fs.ReadFile(h.staticFS, "static/manifest.json")
		if err == nil {
			w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
			w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}

// HandleServiceWorker 返回 PWA Service Worker 脚本。
func (h *Handler) HandleServiceWorker(w http.ResponseWriter, r *http.Request) {
	if h.staticFS != nil {
		data, err := fs.ReadFile(h.staticFS, "static/sw.js")
		if err == nil {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}
