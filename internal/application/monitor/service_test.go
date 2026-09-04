package monitor

import (
	"errors"
	"strings"
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

func (m *memoryRepo) SaveRawKey(key task.TaskKey, data []byte) error {
	return nil
}

func (m *memoryRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

func (m *memoryRepo) DeleteKey(key task.TaskKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, key.TaskID)
	delete(m.tasks, key.String())
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
	// 在真正收口前，任务保持 running 与 abort_requested，不会提前假装已结束
	if hookRes.Task.Status != "running" || hookRes.Task.ControlState != "abort_requested" {
		t.Fatalf("expected task to remain running/abort_requested while intercepting, got status=%s controlState=%s", hookRes.Task.Status, hookRes.Task.ControlState)
	}

	// 真正的终态收口（如 stop / agentCompletion）到来时正式转为 aborted 终态
	stopRes, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-abort-test",
		Agent:     "Cursor Agent",
		Event:     "stop",
		Detail:    "任务执行完成",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("HandleHookEvent stop failed: %v", err)
	}
	if stopRes.Task.Status != "failed" || stopRes.Task.ControlState != "aborted" {
		t.Fatalf("expected task to be terminated as failed/aborted on stop, got status=%s controlState=%s", stopRes.Task.Status, stopRes.Task.ControlState)
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
		Prompt:    "expired work",
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

	// 8. Test Targeted Subagent Steer Injection
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-steer-swarm",
		Agent:     "ZCode",
		Event:     "sessionStart",
		Title:     "Targeted Steer Test",
		Timestamp: time.Now().Unix(),
	})

	// 向特定子智能体类型 Explore 注入指导
	_, err = svc.InjectSteerTargetedTenant("sess-steer-swarm", task.SteerInstruction{
		Message:            "不要扫描 vendor 目录",
		TargetSubagentType: "Explore",
	}, "", true)
	if err != nil {
		t.Fatalf("InjectSteerTargetedTenant failed: %v", err)
	}

	// 常规 Shell 工具执行，TargetSubagentType 为 Explore 的指令不应该被误消费
	resRegular, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-steer-swarm",
		Event:     "beforeShellExecution",
		Detail:    "go test ./...",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("regular hook failed: %v", err)
	}
	if resRegular.AdditionalContext != "" {
		t.Fatalf("regular tool should not consume subagent targeted steer, got %q", resRegular.AdditionalContext)
	}

	// 派发或者执行 Explore 子任务时，精准匹配并消费该指导
	resSubagent, err := svc.HandleHookEvent(task.EventPayload{
		ID:           "sess-steer-swarm",
		Event:        "subagentStart",
		SubagentType: "Explore",
		Detail:       "派发子智能体 [Explore]",
		Timestamp:    time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("subagent hook failed: %v", err)
	}
	if resSubagent.AdditionalContext != "不要扫描 vendor 目录" {
		t.Fatalf("expected targeted steer instruction consumed, got %q", resSubagent.AdditionalContext)
	}
}

func TestMonitorService_IgnoresEmptyCursorOpenClose(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil)

	res, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-idle-open",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("empty sessionStart should still fail-safe: %v", err)
	}
	if res.Task != nil || svc.GetTask("sess-idle-open") != nil {
		t.Fatal("opening a Cursor agent without a prompt must not create a board card")
	}

	_, err = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-idle-open",
		Agent:     "Cursor Agent",
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("empty sessionEnd should still fail-safe: %v", err)
	}
	if svc.GetTask("sess-idle-open") != nil {
		t.Fatal("closing an unused Cursor agent must not leave a completed card")
	}

	_, err = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-idle-open",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Prompt:    "这次真正开始干活",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("prompted sessionStart failed: %v", err)
	}
	created := svc.GetTask("sess-idle-open")
	if created == nil || created.Prompt != "这次真正开始干活" {
		t.Fatalf("same session id with a real prompt must create a task, got %+v", created)
	}
}

func TestMonitorService_DiscardsIdleGhostOnCloseAndRestore(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	ghost := task.NewTask(task.EventPayload{
		ID:    "sess-ghost",
		Agent: "Cursor Agent",
		Event: "sessionStart",
	}, 1_000_000)
	ghost.ApplyEvent(task.EventPayload{
		ID:     "sess-ghost",
		Event:  "sessionStart",
		Detail: "会话启动，分析任务中...",
	}, 1_000_000, "10:00:00")
	repo.tasks[ghost.ID] = ghost

	svc := NewMonitorService(repo, nil)
	if svc.GetTask("sess-ghost") != nil {
		t.Fatal("idle ghost must be discarded when restoring from disk")
	}
	if _, ok := repo.tasks["sess-ghost"]; ok {
		t.Fatal("idle ghost must be deleted from the repository")
	}

	live := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, nil)
	idle := task.NewTask(task.EventPayload{
		ID:    "sess-live-ghost",
		Agent: "Cursor Agent",
		Event: "sessionStart",
	}, 2_000_000)
	live.mu.Lock()
	live.tasks[idle.TaskKey()] = idle
	live.mu.Unlock()

	_, err := live.HandleHookEvent(task.EventPayload{
		ID:        "sess-live-ghost",
		Event:     "agentCompletion",
		Timestamp: 3,
	})
	if err != nil {
		t.Fatalf("closing idle in-memory session: %v", err)
	}
	if live.GetTask("sess-live-ghost") != nil {
		t.Fatal("closing an idle in-memory session must discard it, not complete it")
	}
}

func TestMonitorService_ToolOnlySessionStillRecords(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil)

	_, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-tools",
		Agent:     "Cursor Agent",
		Event:     "beforeShellExecution",
		Detail:    "ls",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("tool event failed: %v", err)
	}
	if svc.GetTask("sess-tools") == nil {
		t.Fatal("first tool use must still open a task even without a captured prompt")
	}

	_, err = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-tools",
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("completion failed: %v", err)
	}
	got := svc.GetTask("sess-tools")
	if got == nil || got.Status != "completed" {
		t.Fatalf("tool-only session should complete, got %+v", got)
	}
}

func TestHandleHookEventTenantIsolation(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil)

	// 1. Project Alpha 启动任务
	_, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-iso-1",
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Title:     "Alpha Task",
		Timestamp: time.Now().Unix(),
	}, "project-alpha", false)
	if err != nil {
		t.Fatalf("alpha session start failed: %v", err)
	}

	taskAlpha := svc.GetTask("sess-iso-1")
	if taskAlpha == nil || taskAlpha.KeyID != "project-alpha" {
		t.Fatalf("expected task with KeyID 'project-alpha', got %+v", taskAlpha)
	}

	// 2. Project Beta 企图通过事件篡改/注入 Project Alpha 的任务
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-iso-1",
		Agent:     "Cursor Agent",
		Event:     "beforeShellExecution",
		Detail:    "rm -rf /",
		Timestamp: time.Now().Unix(),
	}, "project-beta", false)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied when project-beta mutates project-alpha task, got %v", err)
	}

	// 3. Project Alpha 自己能够正常更新
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-iso-1",
		Agent:     "Cursor Agent",
		Event:     "beforeShellExecution",
		Detail:    "go test ./...",
		Timestamp: time.Now().Unix(),
	}, "project-alpha", false)
	if err != nil {
		t.Fatalf("alpha update should succeed, got %v", err)
	}

	// 4. Master 能够合法更新该任务
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-iso-1",
		Agent:     "Cursor Agent",
		Event:     "afterAgentResponse",
		Detail:    "Done",
		Timestamp: time.Now().Unix(),
	}, "project-alpha", true)
	if err != nil {
		t.Fatalf("master update should succeed, got %v", err)
	}

	// 5. 匿名/空租户身份（非 Master）企图篡改或通过终端 Hook 销毁已属于 project-alpha 的任务，必须坚决拒绝
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-iso-1",
		Agent:     "Cursor Agent",
		Event:     "sessionEnd",
		Detail:    "illegal destroy",
		Timestamp: time.Now().Unix(),
	}, "", false)
	if err == nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied when anonymous caller mutates project-alpha task, got %v", err)
	}
		// 验证任务并未被非法篡改或销毁
		if svc.GetTask("sess-iso-1") == nil {
			t.Fatal("task was illegally deleted by anonymous terminal event")
		}
	}

func TestMonitorService_MultiTenantSameSessionIDIsolation(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	hub := NewHub()
	go hub.Run()

	svc := NewMonitorService(repo, hub)
	defer svc.Close()

	sharedID := "sess-duplicate-999"

	// 1. Tenant A 启动同名会话
	_, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        sharedID,
		Agent:     "Cursor Agent",
		Event:     "sessionStart",
		Title:     "Tenant A Workspace",
		Prompt:    "Task for tenant A",
		Timestamp: time.Now().Unix(),
	}, "tenant-a", false)
	if err != nil {
		t.Fatalf("tenant A start failed: %v", err)
	}

	// 2. Tenant B 启动相同 ID 的独立会话
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        sharedID,
		Agent:     "ZCode",
		Event:     "sessionStart",
		Title:     "Tenant B Workspace",
		Prompt:    "Task for tenant B",
		Timestamp: time.Now().Unix(),
	}, "tenant-b", false)
	if err != nil {
		t.Fatalf("tenant B start failed: %v", err)
	}

	// 3. 验证各自在自身租户空间下读取到的是独立对象，无覆盖
	taskA := svc.GetTaskTenant(sharedID, "tenant-a", false)
	if taskA == nil || taskA.KeyID != "tenant-a" || taskA.Title != "Tenant A Workspace" {
		t.Fatalf("unexpected task A: %+v", taskA)
	}

	taskB := svc.GetTaskTenant(sharedID, "tenant-b", false)
	if taskB == nil || taskB.KeyID != "tenant-b" || taskB.Title != "Tenant B Workspace" {
		t.Fatalf("unexpected task B: %+v", taskB)
	}

	// 4. Master 视图同时包含两个独立任务
	masterList := svc.GetAllTasksTenant("", true)
	if len(masterList) != 2 {
		t.Fatalf("master should see 2 tasks, got %d", len(masterList))
	}

	// 5. 并发更新各自任务，验证零数据竞争与状态隔离
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = svc.HandleHookEventTenant(task.EventPayload{
				ID:        sharedID,
				Agent:     "Cursor Agent",
				Event:     "beforeShellExecution",
				Detail:    "echo A",
				Timestamp: time.Now().Unix(),
			}, "tenant-a", false)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = svc.HandleHookEventTenant(task.EventPayload{
				ID:        sharedID,
				Agent:     "ZCode",
				Event:     "beforeShellExecution",
				Detail:    "echo B",
				Timestamp: time.Now().Unix(),
			}, "tenant-b", false)
		}
	}()
	wg.Wait()

	// 6. Tenant A 删除自己的同名任务，Tenant B 任务必须不受影响
	deleted := svc.DeleteTasksTenant(DeleteTasksRequest{IDs: []string{sharedID}}, "tenant-a", false)
	if len(deleted) != 1 || deleted[0] != sharedID {
		t.Fatalf("tenant A delete failed: %+v", deleted)
	}

	if svc.GetTaskTenant(sharedID, "tenant-a", false) != nil {
		t.Fatal("tenant A task should be deleted")
	}
	taskBAfter := svc.GetTaskTenant(sharedID, "tenant-b", false)
	if taskBAfter == nil || taskBAfter.KeyID != "tenant-b" {
		t.Fatalf("tenant B task must still exist and be intact, got %+v", taskBAfter)
	}
}
