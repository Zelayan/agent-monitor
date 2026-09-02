package monitor

import (
	"sync"
	"testing"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

type memoryRepo struct {
	mu    sync.Mutex
	tasks map[string]*task.Task
}

func (m *memoryRepo) FindAll() ([]*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*task.Task
	for _, t := range m.tasks {
		list = append(list, t)
	}
	return list, nil
}

func (m *memoryRepo) Save(t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}

func (m *memoryRepo) SaveRaw(id string, data []byte) error {
	return nil
}

func (m *memoryRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

func (m *memoryRepo) Close() error {
	return nil
}

func TestMonitorService_Orchestration(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	hub := NewHub()
	go hub.Run()

	svc := NewMonitorService(repo, hub)

	// 1. Ingest event
	_, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-app-1",
		Agent:     "ZCode",
		Event:     "SessionStart",
		Title:     "App Service Test",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("HandleHookEvent failed: %v", err)
	}

	tasks := svc.GetAllTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// 2. Mark complete
	_, err = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-app-1",
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("HandleHookEvent completion failed: %v", err)
	}

	// 3. Clear completed
	cleared := svc.ClearFinishedTasks()
	if cleared != 1 {
		t.Errorf("expected 1 cleared task, got %d", cleared)
	}

	if len(svc.GetAllTasks()) != 0 {
		t.Errorf("expected 0 tasks after clear, got %d", len(svc.GetAllTasks()))
	}

	// 4. Test Abort and Soft Deny Inversion
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-abort-test",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Title:     "Abort Test",
		Timestamp: time.Now().Unix(),
	})

	abortedTask, err := svc.AbortTask("sess-abort-test", "Stopped from UI")
	if err != nil {
		t.Fatalf("AbortTask failed: %v", err)
	}
	if abortedTask.ControlState != "abort_requested" {
		t.Fatalf("expected controlState abort_requested, got %s", abortedTask.ControlState)
	}

	// Next preToolUse hook should be denied
	hookRes, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-abort-test",
		Agent:     "Cursor Agent",
		Event:     "preToolUse",
		Detail:    "Running dangerous command",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("HandleHookEvent after abort failed: %v", err)
	}
	if hookRes.Action != "deny" {
		t.Fatalf("expected action 'deny', got %q", hookRes.Action)
	}
	if hookRes.Task.Status != "failed" || hookRes.Task.ControlState != "aborted" {
		t.Fatalf("expected task to be terminated as failed/aborted, got status=%s controlState=%s", hookRes.Task.Status, hookRes.Task.ControlState)
	}
}
