package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

func TestJSONRepository_Lifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository failed: %v", err)
	}
	defer repo.Close()

	taskObj := &task.Task{
		ID:       "sess-repo-test",
		Agent:    "Cursor",
		Repo:     "test/infra",
		Status:   "running",
		RootGoal: "Infrastructure test",
	}

	// 1. Save (默认租户)
	if err := repo.Save(taskObj); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 2. FindAll
	list, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != "sess-repo-test" {
		t.Fatalf("unexpected task list: %+v", list)
	}

	// 3. Delete
	if err := repo.Delete("sess-repo-test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	listAfterDel, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll after del failed: %v", err)
	}
	if len(listAfterDel) != 0 {
		t.Fatalf("expected empty list after del, got %d", len(listAfterDel))
	}
}

func TestJSONRepository_MultiTenantIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-tenant-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository failed: %v", err)
	}
	defer repo.Close()

	// 两个租户保存完全相同的 Session ID
	t1 := &task.Task{
		ID:       "sess-shared-01",
		KeyID:    "tenant-alpha",
		Agent:    "AgentA",
		RootGoal: "Alpha goal",
	}
	t2 := &task.Task{
		ID:       "sess-shared-01",
		KeyID:    "tenant-beta",
		Agent:    "AgentB",
		RootGoal: "Beta goal",
	}

	data1, _ := json.Marshal(t1)
	data2, _ := json.Marshal(t2)

	if err := repo.SaveRawKey(t1.TaskKey(), data1); err != nil {
		t.Fatalf("SaveRawKey t1 failed: %v", err)
	}
	if err := repo.SaveRawKey(t2.TaskKey(), data2); err != nil {
		t.Fatalf("SaveRawKey t2 failed: %v", err)
	}

	// 检查磁盘落盘路径互不干扰
	path1 := repo.taskKeyPath(t1.TaskKey())
	path2 := repo.taskKeyPath(t2.TaskKey())
	if path1 == path2 {
		t.Fatalf("paths should not collide: %s == %s", path1, path2)
	}
	if _, err := os.Stat(path1); err != nil {
		t.Fatalf("path1 file not found: %s", path1)
	}
	if _, err := os.Stat(path2); err != nil {
		t.Fatalf("path2 file not found: %s", path2)
	}

	// FindAll 应能同时加载两个不同租户的任务
	all, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}

	// 删除 tenant-alpha 不影响 tenant-beta
	if err := repo.DeleteKey(t1.TaskKey()); err != nil {
		t.Fatalf("DeleteKey t1 failed: %v", err)
	}
	if _, err := os.Stat(path1); !os.IsNotExist(err) {
		t.Fatalf("path1 should be deleted: %s", path1)
	}
	if _, err := os.Stat(path2); err != nil {
		t.Fatalf("path2 should still exist: %s", path2)
	}
}

func TestJSONRepository_LegacyMigration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-migration-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 手工在根目录下创建一个旧格式的 session 文件
	oldFile := filepath.Join(tmpDir, "sess_legacy.json")
	oldTask := &task.Task{
		ID:       "sess_legacy",
		KeyID:    "tenant-migrated",
		Agent:    "Cursor",
		RootGoal: "Legacy task to be migrated",
	}
	oldData, _ := json.Marshal(oldTask)
	if err := os.WriteFile(oldFile, oldData, 0644); err != nil {
		t.Fatalf("failed to write old file: %v", err)
	}

	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository failed: %v", err)
	}
	defer repo.Close()

	// 执行 FindAll，触发平滑迁移
	tasks, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "sess_legacy" || tasks[0].KeyID != "tenant-migrated" {
		t.Fatalf("unexpected migrated task: %+v", tasks)
	}

	// 确认旧根目录下的文件已被迁移走
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("legacy file %s should have been moved", oldFile)
	}
	// 确认新目录下的文件存在
	newPath := repo.taskKeyPath(oldTask.TaskKey())
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new migrated file %s not found", newPath)
	}
}

func TestJSONRepository_UnicodePathHandling(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-unicode-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository failed: %v", err)
	}
	defer repo.Close()

	// 使用中英文混合 ID 和租户名称
	taskObj := &task.Task{
		ID:       "任务-支付模块-001",
		KeyID:    "研发组-Alpha",
		Agent:    "Cursor",
		RootGoal: "多语言路径与存储测试",
	}

	if err := repo.Save(taskObj); err != nil {
		t.Fatalf("Save unicode task failed: %v", err)
	}

	all, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if len(all) != 1 || all[0].ID != "任务-支付模块-001" || all[0].KeyID != "研发组-Alpha" {
		t.Fatalf("unexpected task after load: %+v", all)
	}
}

func TestJSONRepository_VersionedAndTombstoneSuppression(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-version-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository failed: %v", err)
	}
	defer repo.Close()

	key := task.NewTaskKey("tenant-order", "sess-v-01")
	v1 := &task.Task{ID: "sess-v-01", KeyID: "tenant-order", Version: 1, Detail: "step 1"}
	v2 := &task.Task{ID: "sess-v-01", KeyID: "tenant-order", Version: 2, Detail: "step 2"}

	d1, _ := json.Marshal(v1)
	d2, _ := json.Marshal(v2)

	// 1. 先写入 v2
	if err := repo.SaveRawKeyVersioned(key, 2, d2); err != nil {
		t.Fatalf("Save v2 failed: %v", err)
	}

	// 2. 滞后的 v1 到达，版本更低，必须被忽略不能覆写磁盘
	if err := repo.SaveRawKeyVersioned(key, 1, d1); err != nil {
		t.Fatalf("Save v1 failed: %v", err)
	}

	loaded, err := repo.FindAll()
	if err != nil || len(loaded) != 1 || loaded[0].Version != 2 || loaded[0].Detail != "step 2" {
		t.Fatalf("v1 must not overwrite v2: got %+v", loaded)
	}

	// 3. 执行带版本的删除（版本 3），生成墓碑
	if err := repo.DeleteKeyVersioned(key, 3); err != nil {
		t.Fatalf("Delete v3 failed: %v", err)
	}

	// 4. 再次到达一个滞后的 v2 Save 请求，必须被版本 3 墓碑压制，文件不得复活
	if err := repo.SaveRawKeyVersioned(key, 2, d2); err != nil {
		t.Fatalf("Save stale v2 failed: %v", err)
	}

	afterStale, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if len(afterStale) != 0 {
		t.Fatalf("stale v2 revived deleted task: %+v", afterStale)
	}

	// 5. 若有更高的版本（版本 4，例如该会话重新开启）到达，墓碑解除并成功落盘
	v4 := &task.Task{ID: "sess-v-01", KeyID: "tenant-order", Version: 4, Detail: "step 4 new"}
	d4, _ := json.Marshal(v4)
	if err := repo.SaveRawKeyVersioned(key, 4, d4); err != nil {
		t.Fatalf("Save v4 failed: %v", err)
	}

	afterV4, err := repo.FindAll()
	if err != nil || len(afterV4) != 1 || afterV4[0].Version != 4 {
		t.Fatalf("v4 should succeed and clear stale tombstone: got %+v", afterV4)
	}

	// 6. 重启 Repository 并通过 FindAll 恢复已落盘版本
	repoRestart, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository restart failed: %v", err)
	}
	defer repoRestart.Close()

	if _, err := repoRestart.FindAll(); err != nil {
		t.Fatalf("FindAll on restart failed: %v", err)
	}

	// 重启后提交旧版本 v3，必须被恢复的 lastVersion 拦截，不得覆盖 v4
	v3 := &task.Task{ID: "sess-v-01", KeyID: "tenant-order", Version: 3, Detail: "stale v3"}
	d3, _ := json.Marshal(v3)
	if err := repoRestart.SaveRawKeyVersioned(key, 3, d3); err != nil {
		t.Fatalf("SaveRawKeyVersioned failed: %v", err)
	}
	reloaded, _ := repoRestart.FindAll()
	if len(reloaded) != 1 || reloaded[0].Version != 4 {
		t.Fatalf("stale v3 overwrote v4 after restart: %+v", reloaded)
	}
}

func TestJSONRepository_QuarantineCorruptedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-quarantine-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 在目录下人为制造一个非法的破坏性 JSON 文件
	brokenPath := filepath.Join(tmpDir, "sess-corrupted.json")
	if err := os.WriteFile(brokenPath, []byte("NOT_VALID_JSON{[[{"), 0644); err != nil {
		t.Fatalf("failed to write broken json: %v", err)
	}

	// 2. 初始化 Repository，并执行 FindAll，验证不会挂掉，而是自动移入 quarantine
	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONRepository failed: %v", err)
	}
	defer repo.Close()

	tasks, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll should succeed even with corrupted file: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 valid tasks, got %d", len(tasks))
	}

	// 验证损坏文件已被移出原位置
	if _, err := os.Stat(brokenPath); !os.IsNotExist(err) {
		t.Fatal("broken file should have been moved from original path")
	}

	// 验证隔离统计
	stats := repo.QuarantineStats()
	if stats.Count != 1 || stats.LastError == "" {
		t.Fatalf("unexpected quarantine stats: %+v", stats)
	}
}

func TestJSONRepository_EventLog(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "repo-eventlog-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	repo, err := NewJSONRepository(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	keyDefault := task.NewTaskKey("default", "sess-log-1")
	keyTenant := task.NewTaskKey("tenant-alpha", "sess-log-1")

	// 1. 验证空读取返回 nil, nil
	logs, err := repo.ReadEventLogs(keyDefault)
	if err != nil || len(logs) != 0 {
		t.Fatalf("expected empty logs on non-existent task, got %v, err: %v", logs, err)
	}

	// 2. 写入两条默认租户事件
	rec1 := task.EventRecord{
		EventID:     "ev-1",
		Sequence:    1,
		Timestamp:   1700000000,
		ReceivedAt:  1700000001,
		Event:       "sessionStart",
		Detail:      "Start",
		TaskStatus:  "running",
		TaskVersion: 1,
	}
	rec2 := task.EventRecord{
		EventID:     "ev-2",
		Sequence:    2,
		Timestamp:   1700000010,
		ReceivedAt:  1700000011,
		Event:       "toolUse",
		Detail:      "Exec",
		TaskStatus:  "running",
		TaskVersion: 2,
	}

	if err := repo.AppendEventLog(keyDefault, rec1); err != nil {
		t.Fatalf("AppendEventLog rec1 failed: %v", err)
	}
	if err := repo.AppendEventLog(keyDefault, rec2); err != nil {
		t.Fatalf("AppendEventLog rec2 failed: %v", err)
	}

	// 3. 读取默认租户事件
	readLogs, err := repo.ReadEventLogs(keyDefault)
	if err != nil {
		t.Fatalf("ReadEventLogs failed: %v", err)
	}
	if len(readLogs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(readLogs))
	}
	if readLogs[0].EventID != "ev-1" || readLogs[1].EventID != "ev-2" {
		t.Fatalf("unexpected readLogs: %+v", readLogs)
	}

	// 4. 验证多租户隔离：相同 TaskID 但不同 Tenant 具有独立事件流水
	recTenant := task.EventRecord{
		EventID:     "ev-tenant-1",
		Sequence:    1,
		Timestamp:   1700000020,
		ReceivedAt:  1700000021,
		Event:       "subagentStart",
		Detail:      "Subagent",
		TaskStatus:  "running",
		TaskVersion: 1,
	}
	if err := repo.AppendEventLog(keyTenant, recTenant); err != nil {
		t.Fatalf("AppendEventLog recTenant failed: %v", err)
	}

	tenantLogs, err := repo.ReadEventLogs(keyTenant)
	if err != nil || len(tenantLogs) != 1 {
		t.Fatalf("expected 1 tenant log, got %d, err: %v", len(tenantLogs), err)
	}
	if tenantLogs[0].EventID != "ev-tenant-1" {
		t.Fatalf("unexpected tenant log: %+v", tenantLogs[0])
	}

	// 确认默认租户依然只有 2 条
	readLogs2, _ := repo.ReadEventLogs(keyDefault)
	if len(readLogs2) != 2 {
		t.Fatalf("expected 2 logs for default tenant, got %d", len(readLogs2))
	}
}
