package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func (m *mockRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
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

		// 4. 查询单任务 GET /api/tasks/{id} 状态已为 failed
		wGet := httptest.NewRecorder()
		mux.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/api/tasks/sess-http-abort", nil))
		if wGet.Code != http.StatusOK {
			t.Fatalf("expected 200 for single task query, got %d", wGet.Code)
		}
		var finalTask task.Task
		json.NewDecoder(wGet.Body).Decode(&finalTask)
		if finalTask.Status != "failed" || finalTask.ControlState != "aborted" {
			t.Fatalf("expected final task status failed and controlState aborted, got status=%s controlState=%s", finalTask.Status, finalTask.ControlState)
		}
	}
