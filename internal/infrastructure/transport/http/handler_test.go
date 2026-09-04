package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Zelayan/agent-monitor/internal/application/monitor"
	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

type mockRepo struct {
	mu    sync.Mutex
	tasks map[string]*task.Task
}

func (m *mockRepo) FindAll() ([]*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*task.Task
	for _, t := range m.tasks {
		list = append(list, t)
	}
	return list, nil
}

func (m *mockRepo) Save(t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}

func (m *mockRepo) SaveRaw(id string, data []byte) error {
	return nil
}

func (m *mockRepo) SaveRawKey(key task.TaskKey, data []byte) error {
	return nil
}

func (m *mockRepo) SaveRawKeyVersioned(key task.TaskKey, version uint64, data []byte) error {
	return nil
}

func (m *mockRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

func (m *mockRepo) DeleteKey(key task.TaskKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, key.TaskID)
	delete(m.tasks, key.String())
	return nil
}

func (m *mockRepo) DeleteKeyVersioned(key task.TaskKey, version uint64) error {
	return m.DeleteKey(key)
}

func (m *mockRepo) Close() error {
	return nil
}

func TestHandler_Endpoints(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	staticHTML := []byte("<html><body>Agent Monitor</body></html>")
	handler := NewHandler(svc, hub, staticHTML)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. Test Index
	reqIndex := httptest.NewRequest(http.MethodGet, "/", nil)
	wIndex := httptest.NewRecorder()
	mux.ServeHTTP(wIndex, reqIndex)
	if wIndex.Code != http.StatusOK || !bytes.Contains(wIndex.Body.Bytes(), []byte("Agent Monitor")) {
		t.Fatalf("expected 200 Index, got %d", wIndex.Code)
	}

	// 2. Test Post Event
	p := task.EventPayload{
		ID:        "sess-http-test",
		Agent:     "ZCode",
		Repo:      "ddd/repo",
		Branch:    "master",
		Event:     "SessionStart",
		Title:     "DDD Refactor",
		Prompt:    "Refactor using DDD",
		Timestamp: time.Now().Unix(),
		Detail:    "Started",
	}
	pBytes, _ := json.Marshal(p)
	reqEvent := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pBytes))
	wEvent := httptest.NewRecorder()
	mux.ServeHTTP(wEvent, reqEvent)
	if wEvent.Code != http.StatusOK {
		t.Fatalf("expected 200 for event, got %d", wEvent.Code)
	}

	// 3. Test Get Tasks
	reqTasks := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	wTasks := httptest.NewRecorder()
	mux.ServeHTTP(wTasks, reqTasks)
	if wTasks.Code != http.StatusOK {
		t.Fatalf("expected 200 for tasks, got %d", wTasks.Code)
	}
	var list []*task.Task
	json.NewDecoder(wTasks.Body).Decode(&list)
	if len(list) != 1 || list[0].ID != "sess-http-test" {
		t.Fatalf("unexpected tasks list: %+v", list)
	}

	// 4. Test Complete and Delete Tasks
	pComp := task.EventPayload{
		ID:        "sess-http-test",
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
	}
	pCompBytes, _ := json.Marshal(pComp)
	reqComp := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pCompBytes))
	wComp := httptest.NewRecorder()
	mux.ServeHTTP(wComp, reqComp)

	reqDel := httptest.NewRequest(http.MethodDelete, "/api/tasks", nil)
	wDel := httptest.NewRecorder()
	mux.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 for delete, got %d", wDel.Code)
	}

	tasksRemaining := svc.GetAllTasks()
	if len(tasksRemaining) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasksRemaining))
	}
}

func TestHandler_StaticFS(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	staticHTML := []byte("<html><body>Agent Monitor</body></html>")

	mockFS := fstest.MapFS{
		"static/vendor/test.js": &fstest.MapFile{
			Data: []byte("console.log('offline');"),
		},
		"static/manifest.json": &fstest.MapFile{
			Data: []byte(`{"name":"AGENT MONITOR"}`),
		},
		"static/sw.js": &fstest.MapFile{
			Data: []byte(`// sw`),
		},
	}

	handler := NewHandler(svc, hub, staticHTML).WithStaticFS(mockFS)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/static/vendor/test.js", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for static file, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("console.log('offline');")) {
		t.Fatalf("unexpected static content: %s", w.Body.String())
	}

	// Test Manifest
	reqManifest := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	wManifest := httptest.NewRecorder()
	mux.ServeHTTP(wManifest, reqManifest)
	if wManifest.Code != http.StatusOK || !bytes.Contains(wManifest.Body.Bytes(), []byte("AGENT MONITOR")) {
		t.Fatalf("expected 200 for /manifest.json, got %d", wManifest.Code)
	}

	// Test Service Worker
	reqSW := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	wSW := httptest.NewRecorder()
	mux.ServeHTTP(wSW, reqSW)
	if wSW.Code != http.StatusOK || !bytes.Contains(wSW.Body.Bytes(), []byte("// sw")) {
		t.Fatalf("expected 200 for /sw.js, got %d", wSW.Code)
	}
}

func TestHandler_APIKeyAuth(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	staticHTML := []byte("<html><body>Dashboard</body></html>")
	apiKey := "secret-token-888"

	handler := NewHandler(svc, hub, staticHTML).WithAPIKey(apiKey)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. 未提供 Key 时访问 /api/event 应该返回 401
	pBytes, _ := json.Marshal(task.EventPayload{ID: "sess-auth", Event: "sessionStart"})
	reqNoAuth := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pBytes))
	wNoAuth := httptest.NewRecorder()
	mux.ServeHTTP(wNoAuth, reqNoAuth)
	if wNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/event, got %d", wNoAuth.Code)
	}

	// 2. 未提供 Key 时访问 /api/tasks (GET/DELETE) 应该返回 401
	reqTasksNoAuth := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	wTasksNoAuth := httptest.NewRecorder()
	mux.ServeHTTP(wTasksNoAuth, reqTasksNoAuth)
	if wTasksNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/tasks GET, got %d", wTasksNoAuth.Code)
	}

	reqDelNoAuth := httptest.NewRequest(http.MethodDelete, "/api/tasks?all=true", nil)
	wDelNoAuth := httptest.NewRecorder()
	mux.ServeHTTP(wDelNoAuth, reqDelNoAuth)
	if wDelNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/tasks DELETE, got %d", wDelNoAuth.Code)
	}

	// 3. 错误的 Key 应该返回 401
	reqBadAuth := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	reqBadAuth.Header.Set("Authorization", "Bearer wrong-key")
	wBadAuth := httptest.NewRecorder()
	mux.ServeHTTP(wBadAuth, reqBadAuth)
	if wBadAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong Bearer key, got %d", wBadAuth.Code)
	}

	// 4. 正确的 Authorization: Bearer <key> 访问 /api/event 成功 (200)
	reqGoodBearer := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pBytes))
	reqGoodBearer.Header.Set("Authorization", "Bearer "+apiKey)
	wGoodBearer := httptest.NewRecorder()
	mux.ServeHTTP(wGoodBearer, reqGoodBearer)
	if wGoodBearer.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid Bearer token, got %d", wGoodBearer.Code)
	}

	// 5. 正确的 X-API-Key 头访问 /api/tasks 成功 (200)
	reqXKey := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	reqXKey.Header.Set("X-API-Key", apiKey)
	wXKey := httptest.NewRecorder()
	mux.ServeHTTP(wXKey, reqXKey)
	if wXKey.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid X-API-Key, got %d", wXKey.Code)
	}

	// 6. 前端 SPA 主页 / 应该始终放行 (200) 供未登录用户输入 Key
	reqIndex := httptest.NewRequest(http.MethodGet, "/", nil)
	wIndex := httptest.NewRecorder()
	mux.ServeHTTP(wIndex, reqIndex)
	if wIndex.Code != http.StatusOK {
		t.Fatalf("expected 200 for dashboard index without key, got %d", wIndex.Code)
	}
}

func TestHandler_AbortEndpoint(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	staticHTML := []byte("<html><body>Dashboard</body></html>")
	handler := NewHandler(svc, hub, staticHTML)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. 创建任务
	pStart := task.EventPayload{
		ID:        "sess-http-abort",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Title:     "To be aborted",
		Timestamp: time.Now().Unix(),
	}
	pStartBytes, _ := json.Marshal(pStart)
	wStart := httptest.NewRecorder()
	mux.ServeHTTP(wStart, httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pStartBytes)))
	if wStart.Code != http.StatusOK {
		t.Fatalf("expected 200 for start event, got %d", wStart.Code)
	}

	// 2. 调用 /api/tasks/{id}/abort
	abortBody, _ := json.Marshal(map[string]string{"reason": "Stop now"})
	wAbort := httptest.NewRecorder()
	mux.ServeHTTP(wAbort, httptest.NewRequest(http.MethodPost, "/api/tasks/sess-http-abort/abort", bytes.NewReader(abortBody)))
	if wAbort.Code != http.StatusOK {
		t.Fatalf("expected 200 for abort endpoint, got %d", wAbort.Code)
	}
	var abortResp map[string]interface{}
	json.NewDecoder(wAbort.Body).Decode(&abortResp)
	if abortResp["control_state"] != "abort_requested" {
		t.Fatalf("expected control_state abort_requested, got %v", abortResp["control_state"])
	}

	// 3. 下一个 Hook 事件上报时应该收到 action: deny
	pTool := task.EventPayload{
		ID:        "sess-http-abort",
		Agent:     "Cursor Agent",
		Event:     "beforeShellExecution",
		Detail:    "rm -rf /",
		Timestamp: time.Now().Unix(),
	}
	pToolBytes, _ := json.Marshal(pTool)
	wTool := httptest.NewRecorder()
	mux.ServeHTTP(wTool, httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pToolBytes)))
	if wTool.Code != http.StatusOK {
		t.Fatalf("expected 200 for tool event, got %d", wTool.Code)
	}
	var toolResp map[string]interface{}
	json.NewDecoder(wTool.Body).Decode(&toolResp)
	if toolResp["action"] != "deny" {
		t.Fatalf("expected action 'deny', got %v", toolResp["action"])
	}

	// 4. 查询单任务 GET /api/tasks/{id} 状态处于 abort_requested 拦截中
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/api/tasks/sess-http-abort", nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for single task query, got %d", wGet.Code)
	}
	var finalTask task.Task
	json.NewDecoder(wGet.Body).Decode(&finalTask)
	if finalTask.Status != "running" || finalTask.ControlState != "abort_requested" {
		t.Fatalf("expected task status running and controlState abort_requested during interception, got status=%s controlState=%s", finalTask.Status, finalTask.ControlState)
	}

	// 4.1 测试动态上下文注入 POST /api/tasks/{id}/steer
	steerPayload := map[string]string{"message": "Please do not touch main.go"}
	steerBytes, _ := json.Marshal(steerPayload)
	wSteer := httptest.NewRecorder()
	mux.ServeHTTP(wSteer, httptest.NewRequest(http.MethodPost, "/api/tasks/sess-http-abort/steer", bytes.NewReader(steerBytes)))
	if wSteer.Code != http.StatusOK {
		t.Fatalf("expected 200 for steer endpoint, got %d", wSteer.Code)
	}

	// 5. 测试 POST /api/tasks/{id}/kill 强杀端点
	wKill := httptest.NewRecorder()
	mux.ServeHTTP(wKill, httptest.NewRequest(http.MethodPost, "/api/tasks/sess-http-abort/kill", nil))
	if wKill.Code != http.StatusOK {
		t.Fatalf("expected 200 for kill endpoint, got %d", wKill.Code)
	}
	var killResp map[string]interface{}
	json.NewDecoder(wKill.Body).Decode(&killResp)
	if killResp["control_state"] != "killed" {
		t.Fatalf("expected control_state 'killed', got %v", killResp["control_state"])
	}
}

func TestHandler_MultiTenantKeyIsolation(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	staticHTML := []byte("<html><body>Dashboard</body></html>")

	// 配置两组项目 Key 和一组全局 Master Key
	handler := NewHandler(svc, hub, staticHTML).
		WithMasterKey("master-secret-root").
		WithProjectKeys("team-alpha=alpha-token-123,team-beta=beta-token-456")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. Team Alpha 上报事件
	pAlpha, _ := json.Marshal(task.EventPayload{
		ID:    "task-alpha-1",
		Agent: "Cursor Agent",
		Event: "sessionStart",
		Title: "Alpha Secret Feature",
	})
	reqAlpha := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pAlpha))
	reqAlpha.Header.Set("Authorization", "Bearer alpha-token-123")
	wAlpha := httptest.NewRecorder()
	mux.ServeHTTP(wAlpha, reqAlpha)
	if wAlpha.Code != http.StatusOK {
		t.Fatalf("expected 200 for team alpha report, got %d", wAlpha.Code)
	}

	// 2. Team Beta 上报事件
	pBeta, _ := json.Marshal(task.EventPayload{
		ID:    "task-beta-1",
		Agent: "ZCode",
		Event: "sessionStart",
		Title: "Beta Private Research",
	})
	reqBeta := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(pBeta))
	reqBeta.Header.Set("Authorization", "Bearer beta-token-456")
	wBeta := httptest.NewRecorder()
	mux.ServeHTTP(wBeta, reqBeta)
	if wBeta.Code != http.StatusOK {
		t.Fatalf("expected 200 for team beta report, got %d", wBeta.Code)
	}

	// 3. Team Alpha GET /api/tasks: 只能看到自己的 task-alpha-1，严禁看到 task-beta-1
	reqGetAlpha := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	reqGetAlpha.Header.Set("Authorization", "Bearer alpha-token-123")
	wGetAlpha := httptest.NewRecorder()
	mux.ServeHTTP(wGetAlpha, reqGetAlpha)
	if wGetAlpha.Code != http.StatusOK {
		t.Fatalf("expected 200 for alpha tasks query, got %d", wGetAlpha.Code)
	}
	var listAlpha []*task.Task
	json.NewDecoder(wGetAlpha.Body).Decode(&listAlpha)
	if len(listAlpha) != 1 || listAlpha[0].ID != "task-alpha-1" {
		t.Fatalf("isolation violation: alpha expected only task-alpha-1, got %+v", listAlpha)
	}

	// 4. Team Beta 越权尝试查询或中断 Team Alpha 的任务，必须返回 404 / 失败
	reqBetaAbortAlpha := httptest.NewRequest(http.MethodPost, "/api/tasks/task-alpha-1/abort", nil)
	reqBetaAbortAlpha.Header.Set("Authorization", "Bearer beta-token-456")
	wBetaAbortAlpha := httptest.NewRecorder()
	mux.ServeHTTP(wBetaAbortAlpha, reqBetaAbortAlpha)
	if wBetaAbortAlpha.Code == http.StatusOK {
		t.Fatalf("isolation breach: team beta should NOT be allowed to abort team alpha's task!")
	}

	// 5. Master Key 全局查看：能看到两个项目的所有任务
	reqGetMaster := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	reqGetMaster.Header.Set("Authorization", "Bearer master-secret-root")
	wGetMaster := httptest.NewRecorder()
	mux.ServeHTTP(wGetMaster, reqGetMaster)
	if wGetMaster.Code != http.StatusOK {
		t.Fatalf("expected 200 for master tasks query, got %d", wGetMaster.Code)
	}
	var listMaster []*task.Task
	json.NewDecoder(wGetMaster.Body).Decode(&listMaster)
	if len(listMaster) != 2 {
		t.Fatalf("master should see both tasks, got %d", len(listMaster))
	}
}

func TestTenantPayloadKeyIDCannotOverrideAuthContext(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()
	svc := monitor.NewMonitorService(repo, hub)
	staticHTML := []byte("<html><body>Dashboard</body></html>")

	handler := NewHandler(svc, hub, staticHTML).
		WithMasterKey("master-token").
		WithProjectKeys("team-alpha=token-alpha,team-beta=token-beta")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. Team Alpha 发送请求，但 payload 内部恶意指定 key_id: "team-beta"
	body, _ := json.Marshal(task.EventPayload{
		ID:    "task-spoof-1",
		KeyID: "team-beta",
		Agent: "Cursor Agent",
		Event: "sessionStart",
		Title: "Spoof Attempt",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token-alpha")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	created := svc.GetTask("task-spoof-1")
	if created == nil {
		t.Fatal("expected task to be created")
	}
	// 验证 KeyID 必须被强制绑定为 team-alpha，不能被恶意客户端篡改为 team-beta
	if created.KeyID != "team-alpha" {
		t.Fatalf("security vulnerability: KeyID was overridden by payload! expected 'team-alpha', got %q", created.KeyID)
	}

	// 2. Team Beta 发送跨租户事件篡改 task-spoof-1
	badBody, _ := json.Marshal(task.EventPayload{
		ID:     "task-spoof-1",
		Agent:  "Cursor Agent",
		Event:  "toolUse",
		Detail: "unauthorized tool execution",
	})
	reqBeta := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(badBody))
	reqBeta.Header.Set("Authorization", "Bearer token-beta")
	wBeta := httptest.NewRecorder()
	mux.ServeHTTP(wBeta, reqBeta)

	if wBeta.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when team-beta attempts to mutate team-alpha's task, got %d", wBeta.Code)
	}

	// 3. Master 能够合法指定 key_id 发送事件
	masterBody, _ := json.Marshal(task.EventPayload{
		ID:    "task-master-delegated-1",
		KeyID: "team-beta",
		Agent: "Codex CLI",
		Event: "sessionStart",
		Title: "Master Delegated Task",
	})
	reqMaster := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(masterBody))
	reqMaster.Header.Set("Authorization", "Bearer master-token")
	wMaster := httptest.NewRecorder()
	mux.ServeHTTP(wMaster, reqMaster)

	if wMaster.Code != http.StatusOK {
		t.Fatalf("expected 200 for master event, got %d", wMaster.Code)
	}
	delegated := svc.GetTask("task-master-delegated-1")
	if delegated == nil || delegated.KeyID != "team-beta" {
		t.Fatalf("expected master to be able to designate 'team-beta', got %+v", delegated)
	}
}

func TestHandler_SSESnapshotReconciliationProtocol(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	handler := NewHandler(svc, hub, []byte("ok"))

	// 预先创建两个会话
	_, _ = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "task-snap-1",
		Agent:     "Cursor",
		Event:     "sessionStart",
		Title:     "Task Snap 1",
		Timestamp: time.Now().Unix(),
	}, "tenant-snap", false)

	_, _ = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "task-snap-2",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Title:     "Task Snap 2",
		Timestamp: time.Now().Unix(),
	}, "tenant-snap", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil).WithContext(ctx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{}, 10)}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.HandleStream(w, req)
	}()

	// 等待首帧 Flush
	select {
	case <-w.flushed:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for initial SSE flush")
	}
	cancel()  // 关闭流
	wg.Wait() // 确保后台 HandleStream 完全退出，杜绝并发读写 Body 数据竞争

	body := w.Body.String()
	if !strings.Contains(body, `"type":"snapshot_start"`) {
		t.Fatalf("expected SSE stream to emit snapshot_start frame, got: %s", body)
	}
	if !strings.Contains(body, `"type":"snapshot_end"`) {
		t.Fatalf("expected SSE stream to emit snapshot_end frame, got: %s", body)
	}
	if !strings.Contains(body, `"generation"`) {
		t.Fatalf("expected SSE stream frames to contain generation, got: %s", body)
	}
	if !strings.Contains(body, `"task-snap-1"`) || !strings.Contains(body, `"task-snap-2"`) {
		t.Fatalf("expected SSE snapshot to include tasks, got: %s", body)
	}
}

func TestHandler_StrictHTTPMethodAndErrorDTO(t *testing.T) {
	repo := &mockRepo{tasks: make(map[string]*task.Task)}
	hub := monitor.NewHub()
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)
	handler := NewHandler(svc, hub, []byte("ok"))

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. GET /api/event 应该返回 405 Method Not Allowed 并带有 Allow 头
	reqGetEvent := httptest.NewRequest(http.MethodGet, "/api/event", nil)
	wGetEvent := httptest.NewRecorder()
	mux.ServeHTTP(wGetEvent, reqGetEvent)

	if wGetEvent.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for GET /api/event, got %d", wGetEvent.Code)
	}
	allowHeader := wGetEvent.Header().Get("Allow")
	if !strings.Contains(allowHeader, "POST") {
		t.Fatalf("expected Allow header to contain POST, got: %s", allowHeader)
	}
	var errResp APIErrorResponse
	if err := json.NewDecoder(wGetEvent.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error DTO: %v", err)
	}
	if errResp.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Fatalf("expected error code METHOD_NOT_ALLOWED, got %s", errResp.Error.Code)
	}

	// 2. PUT /api/tasks 应该返回 405 并带有 Allow: GET, DELETE, OPTIONS
	reqPutTasks := httptest.NewRequest(http.MethodPut, "/api/tasks", nil)
	wPutTasks := httptest.NewRecorder()
	mux.ServeHTTP(wPutTasks, reqPutTasks)

	if wPutTasks.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for PUT /api/tasks, got %d", wPutTasks.Code)
	}
	allowTasks := wPutTasks.Header().Get("Allow")
	if !strings.Contains(allowTasks, "GET") || !strings.Contains(allowTasks, "DELETE") {
		t.Fatalf("expected Allow to contain GET, DELETE, got %s", allowTasks)
	}

	// 3. GET /api/tasks/{id}/abort 应该返回 405
	reqGetAbort := httptest.NewRequest(http.MethodGet, "/api/tasks/sess-123/abort", nil)
	wGetAbort := httptest.NewRecorder()
	mux.ServeHTTP(wGetAbort, reqGetAbort)

	if wGetAbort.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET /api/tasks/{id}/abort, got %d", wGetAbort.Code)
	}

	// 4. POST 畸形 JSON 应该返回 400 INVALID_JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/event", strings.NewReader("{bad json"))
	wBadJSON := httptest.NewRecorder()
	mux.ServeHTTP(wBadJSON, reqBadJSON)

	if wBadJSON.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", wBadJSON.Code)
	}
	var badJSONResp APIErrorResponse
	if err := json.NewDecoder(wBadJSON.Body).Decode(&badJSONResp); err != nil || badJSONResp.Error.Code != "INVALID_JSON" {
		t.Fatalf("expected INVALID_JSON error code, got %+v", badJSONResp)
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
}

func (f *flushRecorder) Flush() {
	select {
	case f.flushed <- struct{}{}:
	default:
	}
}
