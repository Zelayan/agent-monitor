package monitor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
	"github.com/Zelayan/agent-monitor/internal/infrastructure/persistence"
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

func (m *memoryRepo) SaveRawKeyVersioned(key task.TaskKey, version uint64, data []byte) error {
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

func (m *memoryRepo) DeleteKeyVersioned(key task.TaskKey, version uint64) error {
	return m.DeleteKey(key)
}

func (m *memoryRepo) ArchiveTask(key task.TaskKey) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, key.TaskID)
	delete(m.tasks, key.String())
	return "mock_archive.tar.gz", nil
}

func (m *memoryRepo) ArchiveCompletedTasks(tenantID string, beforeTime time.Time) ([]task.ArchiveResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []task.ArchiveResult{{Key: task.NewTaskKey(tenantID, "sess-mock"), ArchivePath: "mock_archive.tar.gz"}}, nil
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

func TestMonitorService_MasterDisambiguationPriority(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil)
	defer svc.Close()

	dupID := "sess-ambiguous-01"

	// 1. 创建 default 租户下的已完成旧任务
	tOld := task.NewTask(task.EventPayload{
		ID:        dupID,
		Agent:     "Cursor",
		Event:     "sessionStart",
		Title:     "Old Default Task",
		Timestamp: 1000,
	}, 1000_000)
	tOld.Status = "completed"
	svc.tasks[tOld.TaskKey()] = tOld

	// 2. 创建 tenant-active 下的正在运行新任务
	tActive := task.NewTask(task.EventPayload{
		ID:        dupID,
		KeyID:     "tenant-active",
		Agent:     "Cursor",
		Event:     "sessionStart",
		Title:     "Active Tenant Task",
		Timestamp: 2000,
	}, 2000_000)
	tActive.Status = "running"
	svc.tasks[tActive.TaskKey()] = tActive

	// 3. Master 未指定租户查询该 ID，必须优先路由到 running 状态的活跃任务，而非陈旧的 default 任务
	matched := svc.GetTask(dupID)
	if matched == nil {
		t.Fatalf("master lookup failed for %s", dupID)
	}
	if matched.KeyID != "tenant-active" || matched.Status != "running" {
		t.Fatalf("expected routing to running task in 'tenant-active', got %+v", matched)
	}

	// 4. Master 发起 Abort 控制操作，必须精确作用于正在运行的活跃任务
	aborted, err := svc.AbortTask(dupID, "Master stopping active session")
	if err != nil {
		t.Fatalf("master abort failed: %v", err)
	}
	if aborted.KeyID != "tenant-active" || aborted.ControlState != "abort_requested" {
		t.Fatalf("master abort hit wrong task: %+v", aborted)
	}
}

func TestMonitorService_GracefulCloseDrainsPersistence(t *testing.T) {
	// 使用真实的文件 Repository 测试排空
	tmpDir, err := os.MkdirTemp("", "svc-drain-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := persistence.NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}
	defer repo.Close()

	svc := NewMonitorService(repo, nil)

	// 快速提交 100 个事件
	for i := 0; i < 100; i++ {
		taskID := fmt.Sprintf("sess-drain-%d", i)
		_, err := svc.HandleHookEventTenant(task.EventPayload{
			ID:        taskID,
			Agent:     "Cursor",
			Event:     "sessionStart",
			Title:     fmt.Sprintf("Drain Task %d", i),
			Prompt:    "Verify all records are flushed on shutdown",
			Timestamp: time.Now().Unix(),
		}, "tenant-drain", false)
		if err != nil {
			t.Fatalf("event %d failed: %v", i, err)
		}
	}

	// 立即发起优雅停机，验证 CloseWithContext 在超时内顺利排空所有管道命令
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := svc.CloseWithContext(ctx); err != nil {
		t.Fatalf("CloseWithContext failed: %v", err)
	}

	// 验证磁盘上完整落盘了 100 个文件，无一遗漏
	all, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if len(all) != 100 {
		t.Fatalf("expected all 100 tasks to be persisted, but got %d", len(all))
	}
}

func TestMonitorService_KillSafetyHostMismatch(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil)
	defer svc.Close()

	// 模拟一个来自远程主机的会话
	remoteTask := task.NewTask(task.EventPayload{
		ID:        "sess-remote-kill-01",
		KeyID:     "tenant-safe",
		Agent:     "Cursor",
		Event:     "sessionStart",
		PID:       99999, // 假定一个不存在或远端机器上的 PID
		PGID:      99999,
		HostID:    "foreign-machine-id-xyz",
		BootID:    "foreign-boot-id-123",
		Timestamp: time.Now().Unix(),
	}, time.Now().UnixMilli())
	remoteTask.Status = "running"
	svc.tasks[remoteTask.TaskKey()] = remoteTask

	// 发起 Kill 操作，由于 HostID/BootID 与当前 Monitor 运行宿主不匹配，必须坚决拒绝并返回 ErrHostMismatch
	_, err := svc.KillTaskTenant("sess-remote-kill-01", "tenant-safe", false)
	if err == nil || !errors.Is(err, ErrHostMismatch) {
		t.Fatalf("expected ErrHostMismatch when killing remote task, got: %v", err)
	}

	// 验证任务状态未被伪造为 killed
	tObj := svc.GetTaskTenant("sess-remote-kill-01", "tenant-safe", false)
	if tObj == nil || tObj.Status != "running" || tObj.ControlState == "killed" {
		t.Fatalf("task state should remain running when kill is rejected, got: %+v", tObj)
	}
}

func TestMonitorService_IdempotencyAndReplay(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil)
	t.Cleanup(svc.Close)

	taskID := "sess-idemp-01"
	tenantID := "tenant-alpha"

	// 1. 发送第一条事件 UserPromptSubmit
	res1, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        taskID,
		KeyID:     tenantID,
		Event:     "UserPromptSubmit",
		Prompt:    "Implement feature X",
		Timestamp: 1700000000,
		EventID:   "evt-100",
	}, tenantID, false)
	if err != nil {
		t.Fatalf("res1 error: %v", err)
	}
	if res1.Task == nil || len(res1.Task.Runs) != 1 {
		t.Fatalf("expected 1 run, got: %+v", res1.Task)
	}

	// 2. 发送 toolUse
	res2, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        taskID,
		KeyID:     tenantID,
		Event:     "preToolUse",
		Detail:    "Executing grep",
		Timestamp: 1700000001,
		EventID:   "evt-101",
	}, tenantID, false)
	if err != nil {
		t.Fatalf("res2 error: %v", err)
	}
	timelineCount := len(res2.Task.Runs[0].Timeline)

	// 3. 重复发送相同的 evt-101，验证幂等防御
	resDup, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        taskID,
		KeyID:     tenantID,
		Event:     "preToolUse",
		Detail:    "Executing grep",
		Timestamp: 1700000001,
		EventID:   "evt-101",
	}, tenantID, false)
	if err != nil {
		t.Fatalf("resDup error: %v", err)
	}
	if resDup.Action != "allow" {
		t.Fatalf("expected allow action on duplicate, got %s", resDup.Action)
	}
	// 时间线项数不应增加
	if len(resDup.Task.Runs[0].Timeline) != timelineCount {
		t.Fatalf("duplicate event should not expand timeline, expected %d got %d",
			timelineCount, len(resDup.Task.Runs[0].Timeline))
	}

	// 4. 发送无 EventID 的事件，测试确定性指纹防抖
	fixedTime := int64(1700000005)
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        taskID,
		KeyID:     tenantID,
		Event:     "toolResult",
		Detail:    "grep done",
		Timestamp: fixedTime,
	}, tenantID, false)
	if err != nil {
		t.Fatalf("toolResult error: %v", err)
	}
	tCurrent := svc.GetTaskTenant(taskID, tenantID, false)
	timelineCount2 := len(tCurrent.Runs[0].Timeline)

	// 重复发送完全相同的无 EventID 事件
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        taskID,
		KeyID:     tenantID,
		Event:     "toolResult",
		Detail:    "grep done",
		Timestamp: fixedTime,
	}, tenantID, false)
	if err != nil {
		t.Fatalf("duplicate toolResult error: %v", err)
	}
	tCurrent2 := svc.GetTaskTenant(taskID, tenantID, false)
	if len(tCurrent2.Runs[0].Timeline) != timelineCount2 {
		t.Fatalf("inferred fingerprint duplicate should not expand timeline, expected %d got %d",
			timelineCount2, len(tCurrent2.Runs[0].Timeline))
	}

	// 5. 校验时序回放接口 GetTaskEventReplayTenant
	replay, err := svc.GetTaskEventReplayTenant(taskID, tenantID, false)
	if err != nil {
		t.Fatalf("GetTaskEventReplayTenant failed: %v", err)
	}
	if len(replay) != 3 {
		t.Fatalf("expected 3 replay records (evt-100, evt-101, inferred toolResult), got %d", len(replay))
	}
	// 验证序列号单调递增
	if replay[0].Sequence != 1 || replay[1].Sequence != 2 || replay[2].Sequence != 3 {
		t.Fatalf("expected sequences 1, 2, 3, got %d, %d, %d",
			replay[0].Sequence, replay[1].Sequence, replay[2].Sequence)
	}
	if replay[0].EventID != "evt-100" || replay[1].EventID != "evt-101" {
		t.Fatalf("unexpected event IDs in replay: %+v", replay)
	}

	// 6. 租户隔离校验
	// 非 Master 跨租户查询应返回 ErrPermissionDenied
	_, err = svc.GetTaskEventReplayTenant(taskID, "other-tenant", false)
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for other-tenant, got %v", err)
	}

	// Master 查询应放行
	masterReplay, err := svc.GetTaskEventReplayTenant(taskID, "", true)
	if err != nil || len(masterReplay) != 3 {
		t.Fatalf("master query should succeed with 3 records, got err=%v, count=%d", err, len(masterReplay))
	}

	// 不存在的任务查询应报错
	_, err = svc.GetTaskEventReplayTenant("non-existent", tenantID, false)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestMonitorService_TenantActiveTaskQuota(t *testing.T) {
	repo := &memoryRepo{tasks: make(map[string]*task.Task)}
	svc := NewMonitorService(repo, nil).WithTenantQuota(2)
	t.Cleanup(svc.Close)

	tenantA := "tenant-alpha"
	tenantB := "tenant-beta"

	// 1. 创建租户 A 的第 1 个活跃会话 -> 成功放行
	res1, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-a-1",
		KeyID:     tenantA,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start task 1",
		Timestamp: 1700000001,
	}, tenantA, false)
	if err != nil || res1.Action != "allow" {
		t.Fatalf("session 1 should be allowed, got res=%+v, err=%v", res1, err)
	}

	// 2. 创建租户 A 的第 2 个活跃会话 -> 成功放行
	res2, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-a-2",
		KeyID:     tenantA,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start task 2",
		Timestamp: 1700000002,
	}, tenantA, false)
	if err != nil || res2.Action != "allow" {
		t.Fatalf("session 2 should be allowed, got res=%+v, err=%v", res2, err)
	}

	// 3. 租户 A 当前活跃数已达配额 (2)，创建第 3 个会话 -> 必须拒绝并返回 429 配额错误
	res3, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-a-3",
		KeyID:     tenantA,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start task 3",
		Timestamp: 1700000003,
	}, tenantA, false)
	if err == nil || !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("session 3 should be rejected with ErrQuotaExceeded, got: res=%+v, err=%v", res3, err)
	}
	if res3.Action != "deny" || !strings.Contains(res3.Reason, "quota exceeded") {
		t.Fatalf("expected Action deny and quota reason, got: %+v", res3)
	}

	// 4. 既有会话后续事件正常放行（配额仅限制新建活跃任务）
	resExisting, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-a-1",
		KeyID:     tenantA,
		Event:     "preToolUse",
		Detail:    "running tool",
		Timestamp: 1700000004,
	}, tenantA, false)
	if err != nil || resExisting.Action != "allow" {
		t.Fatalf("existing session event should be allowed, got res=%+v, err=%v", resExisting, err)
	}

	// 5. 租户 B 的并发不受租户 A 影响
	resB, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-b-1",
		KeyID:     tenantB,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start tenant B task",
		Timestamp: 1700000005,
	}, tenantB, false)
	if err != nil || resB.Action != "allow" {
		t.Fatalf("tenant B session should be allowed independently, got res=%+v, err=%v", resB, err)
	}

	// 6. 会话 1 完结，活跃数由 2 降为 1
	_, _ = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-a-1",
		KeyID:     tenantA,
		Event:     "agentCompletion",
		Timestamp: 1700000006,
	}, tenantA, false)

	// 7. 活跃数已释放，再次创建会话 3 -> 成功放行
	res3Retry, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-a-3",
		KeyID:     tenantA,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Retry start task 3",
		Timestamp: 1700000007,
	}, tenantA, false)
	if err != nil || res3Retry.Action != "allow" {
		t.Fatalf("session 3 retry should be allowed after quota released, got res=%+v, err=%v", res3Retry, err)
	}

	// 8. 验证默认租户 (空 KeyID / default) 下同样生效且租户 ID 正确规范化
	resDef1, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-def-1",
		KeyID:     "",
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start default 1",
		Timestamp: 1700000008,
	}, "", false)
	if err != nil || resDef1.Action != "allow" {
		t.Fatalf("default session 1 should be allowed, got res=%+v, err=%v", resDef1, err)
	}

	resDef2, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-def-2",
		KeyID:     "",
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start default 2",
		Timestamp: 1700000009,
	}, "", false)
	if err != nil || resDef2.Action != "allow" {
		t.Fatalf("default session 2 should be allowed, got res=%+v, err=%v", resDef2, err)
	}

	resDef3, err := svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-def-3",
		KeyID:     "",
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Start default 3",
		Timestamp: 1700000010,
	}, "", false)
	if err == nil || !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("default session 3 should be rejected with ErrQuotaExceeded, got res=%+v, err=%v", resDef3, err)
	}
}

func TestMonitorService_ArchiveTaskOrchestration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "svc-archive-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := persistence.NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	hub := NewHub()
	go hub.Run()

	svc := NewMonitorService(repo, hub)
	t.Cleanup(svc.Close)

	tenantID := "tenant-svc-arch"

	// 1. 创建任务 sess-running 并保持运行中
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-running",
		KeyID:     tenantID,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Long running task",
		Timestamp: 1700000000,
	}, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}

	// 2. 尝试归档处于 running 状态的任务 -> 必须拒绝
	_, err = svc.ArchiveTaskTenant("sess-running", tenantID, false)
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("expected error archiving running task, got: %v", err)
	}

	// 3. 创建并完结任务 sess-comp
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-comp",
		KeyID:     tenantID,
		Event:     "sessionStart",
		Agent:     "TestAgent",
		Prompt:    "Real user task to complete",
		Timestamp: 1700000010,
	}, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.HandleHookEventTenant(task.EventPayload{
		ID:        "sess-comp",
		KeyID:     tenantID,
		Event:     "agentCompletion",
		Timestamp: 1700000020,
	}, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}

	// 4. 越权归档校验：非 Master 跨租户归档应拒绝
	_, err = svc.ArchiveTaskTenant("sess-comp", "other-tenant", false)
	if err == nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied when other tenant attempts to archive, got: %v", err)
	}

	// 5. 合法归档 sess-comp (立即触发归档，检验与异步管道的竞态防御与墓碑压制)
	archivePath, err := svc.ArchiveTaskTenant("sess-comp", tenantID, false)
	if err != nil {
		t.Fatalf("ArchiveTaskTenant failed: %v", err)
	}
	if !strings.HasSuffix(archivePath, ".tar.gz") {
		t.Fatalf("expected .tar.gz archive path, got: %s", archivePath)
	}

	// 等待持久化队列充分排空
	time.Sleep(100 * time.Millisecond)

	// 验证已归档任务不会被排队的异步 opSave 滞后覆写复活
	_ = filepath.Walk(tmpDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".json") && strings.Contains(info.Name(), "sess-comp") {
			t.Fatalf("archived task raw file revived by lagging persist: %s", p)
		}
		return nil
	})

	// 6. 验证任务已从内存 tasks map 中清除
	if tObj := svc.GetTaskTenant("sess-comp", tenantID, false); tObj != nil {
		t.Fatalf("archived task should be evicted from memory, got: %+v", tObj)
	}
}
