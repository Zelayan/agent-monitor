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
