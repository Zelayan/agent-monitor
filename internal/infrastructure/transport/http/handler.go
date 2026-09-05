package http

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Zelayan/agent-monitor/internal/application/monitor"
	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

// AuthContext 封装经过鉴权识别后的租户空间身份。
type AuthContext struct {
	KeyID    string // 归属的项目空间名称（如 "proj-a"）
	IsMaster bool   // 是否为 Master 全局管理员
}

// Handler 提供 Monitor 的 HTTP API 与前端页面路由处理。
type Handler struct {
	svc         *monitor.MonitorService
	hub         *monitor.Hub
	staticHTML  []byte
	staticFS    fs.FS
	apiKey      string            // 单 Key 或默认 Key
	masterKey   string            // Master 全局管理 Key
	projectKeys map[string]string // keyHash/token -> keyID/projectName 映射
	allowedCORS []string          // 可配置 CORS 白名单域名（为空则允许全部 "*"）
}

// NewHandler 创建 HTTP 处理器实例。
func NewHandler(svc *monitor.MonitorService, hub *monitor.Hub, staticHTML []byte) *Handler {
	return &Handler{
		svc:         svc,
		hub:         hub,
		staticHTML:  staticHTML,
		projectKeys: make(map[string]string),
	}
}

// WithAllowedCORS 设置允许的 CORS Origin 白名单域名。
func (h *Handler) WithAllowedCORS(origins []string) *Handler {
	h.allowedCORS = origins
	return h
}

// WithAPIKey 设置访问 API Key，非空时对 /api/* 接口启用鉴权。
func (h *Handler) WithAPIKey(apiKey string) *Handler {
	h.apiKey = strings.TrimSpace(apiKey)
	return h
}

// WithMasterKey 设置全局 Master Key。
func (h *Handler) WithMasterKey(masterKey string) *Handler {
	h.masterKey = strings.TrimSpace(masterKey)
	return h
}

// WithProjectKeys 设置多项目 Key 映射列表（格式支持 "projA=key_1,projB=key_2" 或直接逗号分隔多 key）。
func (h *Handler) WithProjectKeys(rawKeys string) *Handler {
	rawKeys = strings.TrimSpace(rawKeys)
	if rawKeys == "" {
		return h
	}
	for _, pair := range strings.Split(rawKeys, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		if idx := strings.Index(pair, "="); idx != -1 {
			projName := strings.TrimSpace(pair[:idx])
			tokenVal := strings.TrimSpace(pair[idx+1:])
			if projName != "" && tokenVal != "" {
				h.projectKeys[tokenVal] = projName
			}
		} else {
			// 未带名字的直接用其 token 或 prefix 作为空间名
			h.projectKeys[pair] = pair
		}
	}
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
	mux.HandleFunc("/api/tasks/", h.HandleTaskDetail)
	mux.HandleFunc("/api/metrics", h.HandleMetrics)
	mux.HandleFunc("/healthz", h.HandleHealthz)
	mux.HandleFunc("/readyz", h.HandleReadyz)
	mux.HandleFunc("/manifest.json", h.HandleManifest)
	mux.HandleFunc("/sw.js", h.HandleServiceWorker)
	if h.staticFS != nil {
		mux.Handle("/static/", http.FileServer(http.FS(h.staticFS)))
	}
	mux.HandleFunc("/", h.HandleIndex)
}

func (h *Handler) enableCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	allowOrigin := "*"
	if len(h.allowedCORS) > 0 {
		allowOrigin = ""
		for _, allowed := range h.allowedCORS {
			if allowed == "*" || allowed == origin {
				allowOrigin = origin
				break
			}
		}
	}
	if allowOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		if allowOrigin != "*" {
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (h *Handler) checkAuth(r *http.Request) (bool, AuthContext) {
	ctx := AuthContext{
		KeyID:    "",
		IsMaster: false,
	}

	// 若未配置任何 apiKey、masterKey 与 projectKeys，处于完全开放模式（单机默认），默认拥有 Master 权限
	if h.apiKey == "" && h.masterKey == "" && len(h.projectKeys) == 0 {
		ctx.IsMaster = true
		ctx.KeyID = ""
		return true, ctx
	}

	token := ""

	// 1. Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			token = strings.TrimSpace(parts[1])
		}
	}

	// 2. X-API-Key: <token>
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}

	// 3. URL Query Parameter ?token=<token> 或 ?api_key=<token> (用于原生 EventSource SSE 长连接)
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("api_key"))
	}

	if token == "" {
		return false, ctx
	}

	// 校验 Master Key
	if h.masterKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.masterKey)) == 1 {
		ctx.IsMaster = true
		ctx.KeyID = "master"
		return true, ctx
	}

	// 校验默认单一 apiKey
	if h.apiKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(h.apiKey)) == 1 {
		ctx.IsMaster = (h.masterKey == "" && len(h.projectKeys) == 0) // 若未开启多租户，单 Key 即全权
		ctx.KeyID = "default"
		return true, ctx
	}

	// 校验多项目 projectKeys 映射
	for pToken, pName := range h.projectKeys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(pToken)) == 1 {
			ctx.IsMaster = false
			ctx.KeyID = pName
			return true, ctx
		}
	}

	return false, ctx
}

func (h *Handler) writeUnauthorized(w http.ResponseWriter) {
	WriteJSONError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Valid API key required")
}

// HandleEvent 处理 Hook POST 上报。
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	if h.enableCORS(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		MethodNotAllowed(w, "POST, OPTIONS")
		return
	}

	ok, authCtx := h.checkAuth(r)
	if !ok {
		h.writeUnauthorized(w)
		return
	}

	// 限制请求体上限为 2MB，防止恶意/异常数据打崩内存
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

	var p task.EventPayload
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&p); err != nil {
		WriteJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON: "+err.Error())
		return
	}

	// 强绑定：非 Master 请求无条件使用已鉴权的租户身份，禁止通过 JSON 载荷篡改 key_id
	if !authCtx.IsMaster {
		p.KeyID = authCtx.KeyID
	} else if p.KeyID == "" && authCtx.KeyID != "" && authCtx.KeyID != "master" {
		p.KeyID = authCtx.KeyID
	}

	res, err := h.svc.HandleHookEventTenant(p, authCtx.KeyID, authCtx.IsMaster)
	if err != nil {
		if errors.Is(err, monitor.ErrPermissionDenied) || strings.Contains(err.Error(), "permission denied") {
			WriteJSONError(w, http.StatusForbidden, "FORBIDDEN", "Forbidden: "+err.Error())
			return
		}
		WriteJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	respData := map[string]interface{}{
		"status": "ok",
		"action": res.Action,
	}
	if res.Reason != "" {
		respData["reason"] = res.Reason
	}
	if res.AdditionalContext != "" {
		respData["additional_context"] = res.AdditionalContext
	}
	if res.AgentMessage != "" {
		respData["agent_message"] = res.AgentMessage
	}
	_ = json.NewEncoder(w).Encode(respData)
}

// HandleStream 提供 SSE 长连接流。
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	if h.enableCORS(w, r) {
		return
	}

	if r.Method != http.MethodGet {
		MethodNotAllowed(w, "GET, OPTIONS")
		return
	}

	ok, authCtx := h.checkAuth(r)
	if !ok {
		h.writeUnauthorized(w)
		return
	}

	flusher, okFlusher := w.(http.Flusher)
	if !okFlusher {
		WriteJSONError(w, http.StatusInternalServerError, "STREAMING_UNSUPPORTED", "Streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := h.hub.SubscribeTenant(authCtx.KeyID, authCtx.IsMaster)
	defer h.hub.Unsubscribe(clientChan)

	// 解析客户端传递的 Last-Event-ID（优先取 HTTP Header，兜底支持 URL Query ?last_event_id=）
	rawLastID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if rawLastID == "" {
		rawLastID = strings.TrimSpace(r.URL.Query().Get("last_event_id"))
	}

	shouldSnapshot := true
	if rawLastID != "" {
		var lastSeq int64
		if _, err := fmt.Sscanf(rawLastID, "%d", &lastSeq); err == nil && lastSeq > 0 {
			// 尝试从 Hub 环形缓冲区重放错过的事件
			missedEvents, canReplay := h.hub.ReplayMissedEvents(lastSeq, authCtx.KeyID, authCtx.IsMaster)
			if canReplay {
				// 成功重放，不需要做全量快照
				shouldSnapshot = false
				for _, ev := range missedEvents {
					fmt.Fprint(w, ev.FormatSSE())
				}
				flusher.Flush()
			} else {
				// 错过的事件已超出环形缓冲区容量，通知客户端执行 resync_required
				resyncFrame, _ := json.Marshal(map[string]interface{}{
					"type":          "resync_required",
					"reason":        "buffer_overflow",
					"last_event_id": lastSeq,
				})
				seq := h.hub.CurrentSeqID()
				resyncEv := monitor.SSEEvent{
					ID:   seq,
					Type: "resync_required",
					Data: string(resyncFrame),
				}
				fmt.Fprint(w, resyncEv.FormatSSE())
				flusher.Flush()
				shouldSnapshot = true
			}
		}
	}

	if shouldSnapshot {
		h.writeSnapshot(w, flusher, authCtx)
	}

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
			// msg 已经是由 Hub 格式化的完整 SSE 帧（含 id, event, data）
			fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}

// writeSnapshot 向 SSE 客户端写入权威快照序列：snapshot_start -> task_upserts -> snapshot_end
func (h *Handler) writeSnapshot(w http.ResponseWriter, flusher http.Flusher, authCtx AuthContext) {
	tasks, gen := h.svc.GetSnapshotWithGeneration(authCtx.KeyID, authCtx.IsMaster)
	currentSeq := h.hub.CurrentSeqID()

	tenantScope := authCtx.KeyID
	if authCtx.IsMaster {
		tenantScope = "*"
	}

	// 下发 snapshot_start
	startFrame, _ := json.Marshal(map[string]interface{}{
		"type":       "snapshot_start",
		"generation": gen,
		"tenant":     tenantScope,
		"isMaster":   authCtx.IsMaster,
		"count":      len(tasks),
	})
	startEv := monitor.SSEEvent{
		ID:   currentSeq,
		Type: "snapshot_start",
		Data: string(startFrame),
	}
	fmt.Fprint(w, startEv.FormatSSE())

	taskIDs := make([]string, 0, len(tasks))
	taskKeys := make([]string, 0, len(tasks))
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.ID)
		taskKeys = append(taskKeys, t.TaskKey().String())
		tData, _ := json.Marshal(t)
		upsertEv := monitor.SSEEvent{
			ID:   currentSeq,
			Type: "task_upsert",
			Data: string(tData),
		}
		fmt.Fprint(w, upsertEv.FormatSSE())
	}

	// 下发 snapshot_end，携带权威全量 ID 与 Key 集合
	endFrame, _ := json.Marshal(map[string]interface{}{
		"type":       "snapshot_end",
		"generation": gen,
		"tenant":     tenantScope,
		"isMaster":   authCtx.IsMaster,
		"taskIds":    taskIDs,
		"taskKeys":   taskKeys,
		"count":      len(tasks),
	})
	endEv := monitor.SSEEvent{
		ID:   currentSeq,
		Type: "snapshot_end",
		Data: string(endFrame),
	}
	fmt.Fprint(w, endEv.FormatSSE())
	flusher.Flush()
}

// HandleTasks 处理任务查询与多种模式删除（全部/选中/已完成）。
func (h *Handler) HandleTasks(w http.ResponseWriter, r *http.Request) {
	if h.enableCORS(w, r) {
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		MethodNotAllowed(w, "GET, DELETE, OPTIONS")
		return
	}

	ok, authCtx := h.checkAuth(r)
	if !ok {
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

		deleted := h.svc.DeleteTasksTenant(req, authCtx.KeyID, authCtx.IsMaster)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cleared":    true,
			"deletedIds": deleted,
			"count":      len(deleted),
		})
		return
	}

	tasks := h.svc.GetAllTasksTenant(authCtx.KeyID, authCtx.IsMaster)
	json.NewEncoder(w).Encode(tasks)
}

// HandleTaskDetail 处理单个任务的具体操作（如 /api/tasks/{id}/abort, /api/tasks/{id}）。
func (h *Handler) HandleTaskDetail(w http.ResponseWriter, r *http.Request) {
	if h.enableCORS(w, r) {
		return
	}

	ok, authCtx := h.checkAuth(r)
	if !ok {
		h.writeUnauthorized(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	subPath := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	parts := strings.Split(strings.Trim(subPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not found")
		return
	}

	taskID := parts[0]

	// 1. /api/tasks/{id}/abort 软中断会话
	if len(parts) == 2 && parts[1] == "abort" {
		if r.Method != http.MethodPost {
			MethodNotAllowed(w, "POST, OPTIONS")
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body)
		}
		abortedTask, err := h.svc.AbortTaskTenant(taskID, body.Reason, authCtx.KeyID, authCtx.IsMaster)
		if err != nil {
			WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "ok",
			"control_state": "abort_requested",
			"task":          abortedTask,
		})
		return
	}

	// 1.05 /api/tasks/{id}/steer 动态向 Agent 注入上下文/指导 (支持定向子智能体)
	if len(parts) == 2 && (parts[1] == "steer" || parts[1] == "inject-context") {
		if r.Method != http.MethodPost {
			MethodNotAllowed(w, "POST, OPTIONS")
			return
		}
		var body struct {
			Context            string `json:"context"`
			Message            string `json:"message"`
			TargetChildID      string `json:"target_child_id"`
			ChildID            string `json:"child_id"`
			TargetSubagentType string `json:"target_subagent_type"`
			SubagentType       string `json:"subagent_type"`
			TargetSubagentID   string `json:"target_subagent_id"`
			SubagentID         string `json:"subagent_id"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body)
		}
		msg := body.Context
		if msg == "" {
			msg = body.Message
		}

		targetChildID := body.TargetChildID
		if targetChildID == "" {
			targetChildID = body.ChildID
		}
		targetSubType := body.TargetSubagentType
		if targetSubType == "" {
			targetSubType = body.SubagentType
		}
		targetSubID := body.TargetSubagentID
		if targetSubID == "" {
			targetSubID = body.SubagentID
		}

		inst := task.SteerInstruction{
			Message:            msg,
			TargetChildID:      targetChildID,
			TargetSubagentType: targetSubType,
			TargetSubagentID:   targetSubID,
		}

		steeredTask, err := h.svc.InjectSteerTargetedTenant(taskID, inst, authCtx.KeyID, authCtx.IsMaster)
		if err != nil {
			WriteJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"task":   steeredTask,
		})
		return
	}

		// 1.1 /api/tasks/{id}/kill 进程级强杀
		if len(parts) == 2 && parts[1] == "kill" {
			if r.Method != http.MethodPost {
				MethodNotAllowed(w, "POST, OPTIONS")
				return
			}
			killedTask, err := h.svc.KillTaskTenant(taskID, authCtx.KeyID, authCtx.IsMaster)
			if err != nil {
				if errors.Is(err, monitor.ErrHostMismatch) {
					WriteJSONError(w, http.StatusBadRequest, "HOST_MISMATCH", err.Error())
					return
				}
				WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":        "ok",
				"control_state": "killed",
				"task":          killedTask,
			})
			return
		}

		// 1.2 /api/tasks/{id}/replay 事件流时序回放
		if len(parts) == 2 && parts[1] == "replay" {
			if r.Method != http.MethodGet {
				MethodNotAllowed(w, "GET, OPTIONS")
				return
			}
			records, err := h.svc.GetTaskEventReplayTenant(taskID, authCtx.KeyID, authCtx.IsMaster)
			if err != nil {
				if errors.Is(err, monitor.ErrPermissionDenied) {
					WriteJSONError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
					return
				}
				WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			if records == nil {
				records = []task.EventRecord{}
			}
			_ = json.NewEncoder(w).Encode(records)
			return
		}

		// 1.3 /api/tasks/replay?id={id} 查询参数形式事件流时序回放
		if len(parts) == 1 && parts[0] == "replay" {
			if r.Method != http.MethodGet {
				MethodNotAllowed(w, "GET, OPTIONS")
				return
			}
			targetID := strings.TrimSpace(r.URL.Query().Get("id"))
			if targetID == "" {
				WriteJSONError(w, http.StatusBadRequest, "BAD_REQUEST", "Missing id query parameter")
				return
			}
			records, err := h.svc.GetTaskEventReplayTenant(targetID, authCtx.KeyID, authCtx.IsMaster)
			if err != nil {
				if errors.Is(err, monitor.ErrPermissionDenied) {
					WriteJSONError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
					return
				}
				WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
				return
			}
			if records == nil {
				records = []task.EventRecord{}
			}
			_ = json.NewEncoder(w).Encode(records)
			return
		}

		// 2. /api/tasks/{id} 单任务查询与删除
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			t := h.svc.GetTaskTenant(taskID, authCtx.KeyID, authCtx.IsMaster)
			if t == nil {
				WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", "Task not found: "+taskID)
				return
			}
			json.NewEncoder(w).Encode(t)
			return
		case http.MethodDelete:
			deleted := h.svc.DeleteTasksTenant(monitor.DeleteTasksRequest{IDs: []string{taskID}}, authCtx.KeyID, authCtx.IsMaster)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"deleted": len(deleted) > 0,
				"id":      taskID,
			})
			return
		default:
			MethodNotAllowed(w, "GET, DELETE, OPTIONS")
			return
		}
	}

	WriteJSONError(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not found")
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

// HandleHealthz 存活探针接口：快速返回 200 OK 表明进程存活。
func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		MethodNotAllowed(w, "GET, HEAD, OPTIONS")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleReadyz 就绪探针接口：检查持久化存储可用性与写管道就绪状态。
func (h *Handler) HandleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		MethodNotAllowed(w, "GET, HEAD, OPTIONS")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if h.svc != nil && h.svc.IsReady() {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ready",
		})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "not_ready",
	})
}

// HandleMetrics 导出服务运行时吞吐与健康状态指标。
func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if h.enableCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		MethodNotAllowed(w, "GET, OPTIONS")
		return
	}
	if h.svc == nil {
		WriteJSONError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Monitor service not ready")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	metrics := h.svc.Metrics()
	_ = json.NewEncoder(w).Encode(metrics)
}
