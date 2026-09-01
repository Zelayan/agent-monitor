package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestHub_StoreIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hub-store-test-*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewJSONStore(tmpDir)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	hub := newHub(store)
	go hub.run()

	// 1. Post a new event
	payload := EventPayload{
		ID:        "sess-test-hub",
		Agent:     "Cursor",
		Repo:      "my/repo",
		Branch:    "main",
		Event:     "sessionStart",
		Title:     "Test Hub Integration",
		Prompt:    "Initial prompt",
		Timestamp: time.Now().Unix(),
		Detail:    "Session started",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(body))
	w := httptest.NewRecorder()
	hub.handleEvent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	// Wait for async persistence
	time.Sleep(50 * time.Millisecond)

	// Check if loaded by a fresh Hub
	hub2 := newHub(store)
	if len(hub2.tasks) != 1 {
		t.Fatalf("expected 1 task restored in hub2, got %d", len(hub2.tasks))
	}
	restoredTask := hub2.tasks["sess-test-hub"]
	if restoredTask == nil || restoredTask.Title != "Test Hub Integration" {
		t.Fatalf("restored task mismatch: %+v", restoredTask)
	}

	// 2. Mark completed and call DELETE /api/tasks
	completePayload := EventPayload{
		ID:        "sess-test-hub",
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
		Detail:    "All done",
	}
	bodyComp, _ := json.Marshal(completePayload)
	reqComp := httptest.NewRequest(http.MethodPost, "/api/event", bytes.NewReader(bodyComp))
	wComp := httptest.NewRecorder()
	hub.handleEvent(wComp, reqComp)

	time.Sleep(50 * time.Millisecond)

	// DELETE
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/tasks", nil)
	wDel := httptest.NewRecorder()
	hub.handleTasks(wDel, reqDel)

	time.Sleep(50 * time.Millisecond)

	// Verify hub3 has 0 tasks
	hub3 := newHub(store)
	if len(hub3.tasks) != 0 {
		t.Fatalf("expected 0 tasks after DELETE in hub3, got %d", len(hub3.tasks))
	}
}
