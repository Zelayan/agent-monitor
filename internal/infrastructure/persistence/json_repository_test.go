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
