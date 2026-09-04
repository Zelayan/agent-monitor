package task

import (
	"testing"
)

func TestTaskKey(t *testing.T) {
	k1 := NewTaskKey("", "sess_01")
	if k1.TenantID != DefaultTenantID {
		t.Fatalf("expected tenant %q, got %q", DefaultTenantID, k1.TenantID)
	}
	if k1.String() != "default:sess_01" {
		t.Fatalf("expected string %q, got %q", "default:sess_01", k1.String())
	}
	if k1.IsZero() {
		t.Fatalf("expected k1 not to be zero")
	}

	k2 := NewTaskKey("  proj-a  ", "sess_02")
	if k2.TenantID != "proj-a" || k2.TaskID != "sess_02" {
		t.Fatalf("unexpected k2 values: %+v", k2)
	}
	if k2.String() != "proj-a:sess_02" {
		t.Fatalf("expected string %q, got %q", "proj-a:sess_02", k2.String())
	}

	zeroKey := NewTaskKey("proj-a", "")
	if !zeroKey.IsZero() {
		t.Fatalf("expected zeroKey to be zero")
	}

	task := &Task{
		ID:    "task-123",
		KeyID: "tenant-x",
	}
	tk := task.TaskKey()
	if tk.TenantID != "tenant-x" || tk.TaskID != "task-123" {
		t.Fatalf("unexpected task key: %+v", tk)
	}
}
