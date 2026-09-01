package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"agent-monitor/internal/domain/task"
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

	// 1. Save
	if err := repo.Save(taskObj); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	expectedFile := filepath.Join(tmpDir, "sess-repo-test.json")
	if _, err := os.Stat(expectedFile); err != nil {
		t.Fatalf("expected file %s not found", expectedFile)
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

func TestJSONRepository_SafeFilename(t *testing.T) {
	if SafeFilename("../../etc/passwd") != ".._.._etc_passwd.json" {
		t.Errorf("unexpected safe filename: %s", SafeFilename("../../etc/passwd"))
	}
}
