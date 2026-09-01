package reporter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetHookResponse(t *testing.T) {
	tests := []struct {
		event    string
		expected string
	}{
		{"beforeSubmitPrompt", `{"continue":true}`},
		{"UserPromptSubmit", `{"continue":true}`},
		{"beforeShellExecution", `{"permission":"allow"}`},
		{"PreToolUse", `{"permission":"allow"}`},
		{"preToolUse", `{"permission":"allow"}`},
		{"PermissionRequest", `{"permission":"allow"}`},
		{"SessionStart", `{}`},
		{"Stop", `{}`},
		{"unknown", `{}`},
	}

	for _, tt := range tests {
		got := GetHookResponse(tt.event)
		if got != tt.expected {
			t.Errorf("GetHookResponse(%q) = %q; want %q", tt.event, got, tt.expected)
		}
	}
}

func TestUnwrapUserQuery(t *testing.T) {
	input := "<user_query>\n  请帮我重构代码\n  <timestamp>2026-09-02</timestamp>\n</user_query>"
	want := "请帮我重构代码"
	got := UnwrapUserQuery(input)
	if got != want {
		t.Errorf("UnwrapUserQuery() = %q; want %q", got, want)
	}

	plain := "普通问题无需解包"
	if UnwrapUserQuery(plain) != plain {
		t.Errorf("UnwrapUserQuery() = %q; want %q", UnwrapUserQuery(plain), plain)
	}
}

func TestShortTitle(t *testing.T) {
	text := "#task [board] 任务: 实现全新的监控看板\n这是第二行详细描述"
	want := "实现全新的监控看板"
	got := ShortTitle(text)
	if got != want {
		t.Errorf("ShortTitle() = %q; want %q", got, want)
	}

	longText := strings.Repeat("长", 100)
	gotLong := ShortTitle(longText)
	if len([]rune(gotLong)) != 80 {
		t.Errorf("ShortTitle long text length = %d; want 80", len([]rune(gotLong)))
	}
}

func TestExtractDetail(t *testing.T) {
	payloadWithCmd := Payload{Command: "go test ./..."}
	if d := ExtractDetail(payloadWithCmd, "PreToolUse", "Bash", false); d != "执行命令: go test ./..." {
		t.Errorf("ExtractDetail(payloadWithCmd) = %q", d)
	}

	payloadWithToolInput := Payload{
		ToolInput: map[string]interface{}{"command": "npm run build"},
	}
	if d := ExtractDetail(payloadWithToolInput, "PreToolUse", "bash", false); d != "执行命令: npm run build" {
		t.Errorf("ExtractDetail(toolInput) = %q", d)
	}

	startPayload := Payload{}
	if d := ExtractDetail(startPayload, "SessionStart", "", false); d != "会话启动，分析任务中..." {
		t.Errorf("ExtractDetail(start) = %q", d)
	}

	failPayload := Payload{Error: "command timed out"}
	if d := ExtractDetail(failPayload, "PostToolUseFailure", "Bash", true); d != "command timed out" {
		t.Errorf("ExtractDetail(failure) = %q", d)
	}
}

func TestSendEvent(t *testing.T) {
	var received EventReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Method = %s; want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s; want application/json", ct)
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	report := EventReport{
		ID:        "sess-12345",
		Agent:     "ZCode",
		Repo:      "agent-monitor:master",
		Event:     "sessionStart",
		Timestamp: 1772580000,
		Detail:    "会话启动",
		TurnIndex: 1,
		Title:     "测试任务",
	}

	SendEvent(server.URL, report)

	if received.ID != "sess-12345" || received.Agent != "ZCode" || received.Title != "测试任务" {
		t.Errorf("Received mismatch: %+v", received)
	}
}

func TestIgnoredToolsFilter(t *testing.T) {
	for _, tool := range []string{"Read", "read", "Glob", "Grep", "TodoWrite", "Edit", "Write"} {
		if !IgnoredTools[tool] {
			t.Errorf("Expected %s to be ignored", tool)
		}
	}
	for _, tool := range []string{"Bash", "bash", "execute_command"} {
		if IgnoredTools[tool] {
			t.Errorf("Expected %s NOT to be ignored", tool)
		}
	}
}

func TestExtractTurnInfo(t *testing.T) {
	payload := Payload{
		Prompt: "<user_query>修复登录 bug</user_query>",
	}
	turns, curr, first := ExtractTurnInfo(payload, "sess-test", 0)
	if turns != 1 {
		t.Errorf("turns = %d; want 1", turns)
	}
	if curr != "修复登录 bug" || first != "修复登录 bug" {
		t.Errorf("prompt = %q, %q", curr, first)
	}
}

func TestParsePayload(t *testing.T) {
	jsonInput := `{"agent":"ZCode","event":"SessionStart","session_id":"sess-999"}`
	payload := parsePayload(bytes.NewBufferString(jsonInput))
	if payload.Agent != "ZCode" || payload.Event != "SessionStart" || payload.SessionID != "sess-999" {
		t.Errorf("Parsed payload mismatch: %+v", payload)
	}

	rawInput := `not a valid json`
	rawPayload := parsePayload(bytes.NewBufferString(rawInput))
	if rawPayload.Raw != "not a valid json" {
		t.Errorf("Raw payload mismatch: %+v", rawPayload)
	}
}
