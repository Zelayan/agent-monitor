package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONStore_BasicLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jsonstore-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	task1 := &Task{
		ID:       "sess-123",
		Agent:    "ZCode",
		Repo:     "test/repo",
		Branch:   "main",
		RootGoal: "Fix bug in store",
		Status:   "running",
		Runs: []Turn{
			{
				Index:  1,
				Title:  "Fix bug in store",
				Status: "running",
				Timeline: []TimelineItem{
					{Time: "10:00:00", Event: "sessionStart", Desc: "Start session"},
				},
			},
		},
	}

	// 1. Save
	if err := store.SaveTask(task1); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Verify file exists
	expectedFile := filepath.Join(tmpDir, "sess-123.json")
	if _, err := os.Stat(expectedFile); err != nil {
		t.Fatalf("expected task file not found: %v", err)
	}

	// 2. LoadAll
	tasks, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "sess-123" || tasks[0].Agent != "ZCode" || len(tasks[0].Runs) != 1 {
		t.Fatalf("unexpected task content loaded: %+v", tasks[0])
	}

	// 3. Update
	task1.Status = "completed"
	task1.Duration = "01m 20s"
	if err := store.SaveTask(task1); err != nil {
		t.Fatalf("Update SaveTask failed: %v", err)
	}

	tasksAfterUpdate, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll after update failed: %v", err)
	}
	if len(tasksAfterUpdate) != 1 || tasksAfterUpdate[0].Status != "completed" {
		t.Fatalf("expected completed status, got: %v", tasksAfterUpdate[0].Status)
	}

	// 4. Delete
	if err := store.DeleteTask("sess-123"); err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}

	tasksAfterDelete, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll after delete failed: %v", err)
	}
	if len(tasksAfterDelete) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasksAfterDelete))
	}
}

func TestJSONStore_SafeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sess_123", "sess_123.json"},
		{"sess-456", "sess-456.json"},
		{"sess/../bad:id", "sess_.._bad_id.json"},
		{"", "unknown.json"},
	}

	for _, tc := range tests {
		got := safeFilename(tc.input)
		if got != tc.expected {
			t.Errorf("safeFilename(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}
