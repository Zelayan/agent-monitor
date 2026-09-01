package monitor

import (
	"sync"
	"testing"
	"time"

	"agent-monitor/internal/domain/task"
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
}
