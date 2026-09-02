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

	// 5. Test KillTask Hard Kill
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-kill-test",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Title:     "Kill Test",
		PID:       99999999, // nonexistent PID
		Timestamp: time.Now().Unix(),
	})

	killedTask, err := svc.KillTask("sess-kill-test")
	if err != nil {
		t.Fatalf("KillTask failed: %v", err)
	}
	if killedTask.Status != "failed" || killedTask.ControlState != "killed" {
		t.Fatalf("expected task to be killed, got status=%s controlState=%s", killedTask.Status, killedTask.ControlState)
	}

	// 6. Test Multi-Tenant KeyID Project Isolation
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-proj-a-1",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Title:     "Project A Work",
		KeyID:     "project-alpha",
		Timestamp: time.Now().Unix(),
	})
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-proj-b-1",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Title:     "Project B Work",
		KeyID:     "project-beta",
		Timestamp: time.Now().Unix(),
	})

	// Tenant Alpha only sees Project Alpha tasks
	tasksAlpha := svc.GetAllTasksTenant("project-alpha", false)
	if len(tasksAlpha) != 1 || tasksAlpha[0].ID != "sess-proj-a-1" {
		t.Fatalf("expected only project-alpha task, got %+v", tasksAlpha)
	}

	// Tenant Beta only sees Project Beta tasks
	tasksBeta := svc.GetAllTasksTenant("project-beta", false)
	if len(tasksBeta) != 1 || tasksBeta[0].ID != "sess-proj-b-1" {
		t.Fatalf("expected only project-beta task, got %+v", tasksBeta)
	}

	// Master sees all tasks
	tasksMaster := svc.GetAllTasksTenant("master", true)
	if len(tasksMaster) < 2 {
		t.Fatalf("expected master to see all tasks, got %d", len(tasksMaster))
	}

	// Cross-tenant abort rejection
	_, err = svc.AbortTaskTenant("sess-proj-a-1", "Hacker abort", "project-beta", false)
	if err == nil {
		t.Fatalf("expected permission denied when project-beta tries to abort project-alpha task")
	}

	// Authorized abort
	abortedAlpha, err := svc.AbortTaskTenant("sess-proj-a-1", "Authorized abort", "project-alpha", false)
	if err != nil || abortedAlpha.ControlState != "abort_requested" {
		t.Fatalf("expected successful abort by project-alpha, got err=%v", err)
	}

	// 7. Test Janitor TTL Clean
	svc.ttlDays = 1 // 1 天前过期
	oldTaskPayload := task.EventPayload{
		ID:        "sess-expired-old",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Timestamp: time.Now().AddDate(0, 0, -5).Unix(), // 5 天前
	}
	_, _ = svc.HandleHookEvent(oldTaskPayload)
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-expired-old",
		Event:     "agentCompletion",
		Timestamp: time.Now().AddDate(0, 0, -5).Unix(),
	})

	svc.cleanExpiredTasks()
	if tObj := svc.GetTask("sess-expired-old"); tObj != nil {
		t.Fatalf("expected expired task to be cleaned up by janitor, but still exists")
	}
}
