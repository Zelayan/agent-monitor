package reporter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetHookResponse(t *testing.T) {
	tests := []struct {
		event    string
		expected string
	}{
		{"beforeSubmitPrompt", `{"continue":true}`},
		{"UserPromptSubmit", `{"continue":true}`},
		{"beforeShellExecution", `{"permission":"allow"}`},
		{"beforeMCPExecution", `{"permission":"allow"}`},
		{"PreToolUse", `{"permission":"allow"}`},
		{"preToolUse", `{"permission":"allow"}`},
		{"subagentStart", `{"permission":"allow"}`},
		{"PermissionRequest", `{"permission":"allow"}`},
		{"SessionStart", `{}`},
		{"afterAgentResponse", `{}`},
		{"stop", `{}`},
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
		ToolInput: json.RawMessage(`{"command":"npm run build"}`),
	}
	if d := ExtractDetail(payloadWithToolInput, "PreToolUse", "bash", false); d != "执行命令: npm run build" {
		t.Errorf("ExtractDetail(toolInput) = %q", d)
	}

	cursorShell := Payload{
		ToolInput: json.RawMessage(`{"command":"ls","working_directory":"/project"}`),
	}
	if d := ExtractDetail(cursorShell, "preToolUse", "Shell", false); d != "执行命令: ls" {
		t.Errorf("ExtractDetail(cursor Shell) = %q", d)
	}

	mcpPayload := Payload{MCPServerName: "cursor-ide-browser", ToolName: "browser_navigate"}
	if d := ExtractDetail(mcpPayload, "beforeMCPExecution", "browser_navigate", false); d != "调用 MCP: browser_navigate" {
		t.Errorf("ExtractDetail(MCP) = %q", d)
	}

	failPayload := Payload{ErrorMessage: "Command timed out after 30s"}
	if d := ExtractDetail(failPayload, "postToolUseFailure", "Shell", true); d != "Command timed out after 30s" {
		t.Errorf("ExtractDetail(cursor failure) = %q", d)
	}

	startPayload := Payload{}
	if d := ExtractDetail(startPayload, "SessionStart", "", false); d != "会话启动，分析任务中..." {
		t.Errorf("ExtractDetail(start) = %q", d)
	}

	failClaude := Payload{Error: "command timed out"}
	if d := ExtractDetail(failClaude, "PostToolUseFailure", "Bash", true); d != "command timed out" {
		t.Errorf("ExtractDetail(failure) = %q", d)
	}

	aiPayload := Payload{Text: "**Agent Monitor 已在 8000 端口启动。**"}
	if d := ExtractDetail(aiPayload, "afterAgentResponse", "", false); d != "AI 回复: **Agent Monitor 已在 8000 端口启动。**" {
		t.Errorf("ExtractDetail(afterAgentResponse) = %q", d)
	}
	if d := ExtractDetail(Payload{}, "beforeSubmitPrompt", "", false); d != "用户提交 Prompt" {
		t.Errorf("ExtractDetail(beforeSubmitPrompt) = %q", d)
	}
	if d := ExtractDetail(Payload{}, "stop", "", false); d != "任务执行完成" {
		t.Errorf("ExtractDetail(stop) = %q", d)
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
	for _, tool := range []string{"Bash", "bash", "execute_command", "Shell", "Task"} {
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

	cursorInput := `{"conversation_id":"conv-1","generation_id":"gen-9","hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"pwd"},"workspace_roots":["/tmp/proj"],"error_message":"","text":"","status":"completed"}`
	cursorPayload := parsePayload(bytes.NewBufferString(cursorInput))
	if cursorPayload.ConversationID != "conv-1" || cursorPayload.ToolName != "Shell" || cursorPayload.HookEventName != "preToolUse" {
		t.Errorf("Cursor payload mismatch: %+v", cursorPayload)
	}
	if extractCommandFromArgs(cursorPayload) != "pwd" {
		t.Errorf("tool_input object command = %q", extractCommandFromArgs(cursorPayload))
	}

	mcpInput := `{"tool_name":"search","tool_input":"{\"query\":\"hooks\"}","mcp_server_name":"docs"}`
	mcpPayload := parsePayload(bytes.NewBufferString(mcpInput))
	if mcpPayload.MCPServerName != "docs" || mcpPayload.ToolName != "search" {
		t.Errorf("MCP payload mismatch: %+v", mcpPayload)
	}
	if m := toolInputAsMap(mcpPayload); m["query"] != "hooks" {
		t.Errorf("MCP tool_input string parse = %#v", m)
	}

	rawInput := `not a valid json`
	rawPayload := parsePayload(bytes.NewBufferString(rawInput))
	if rawPayload.Raw != "not a valid json" {
		t.Errorf("Raw payload mismatch: %+v", rawPayload)
	}
}

func TestDetectAgentName(t *testing.T) {
	if name := DetectAgentName("CustomAgent", ""); name != "CustomAgent" {
		t.Errorf("expected CustomAgent, got %s", name)
	}
	if name := DetectAgentName("", "PayloadAgent"); name != "PayloadAgent" {
		t.Errorf("expected PayloadAgent, got %s", name)
	}
	// Test environment variable detection
	t.Setenv("CLAUDE_SESSION_ID", "claude-123")
	if name := DetectAgentName("", ""); name != "Claude Code" {
		t.Errorf("expected Claude Code, got %s", name)
	}

	// Test Codex CLI and Desktop detection
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CODEX_SESSION_ID", "codex-sess-123")
	if name := DetectAgentName("", ""); name != "Codex CLI" {
		t.Errorf("expected Codex CLI, got %s", name)
	}

	t.Setenv("CODEX_DESKTOP_VERSION", "1.0.0")
	if name := DetectAgentName("", ""); name != "Codex Desktop" {
		t.Errorf("expected Codex Desktop, got %s", name)
	}
}

func TestExtractSessionID(t *testing.T) {
	p := Payload{TaskID: "task-abc"}
	if id := ExtractSessionID(p); id != "task-abc" {
		t.Errorf("expected task-abc, got %s", id)
	}
	cursor := Payload{ConversationID: "conv-stable", GenerationID: "gen-turn", SessionID: "sess-same"}
	if id := ExtractSessionID(cursor); id != "conv-stable" {
		t.Errorf("expected conversation_id over generation_id, got %s", id)
	}
	t.Setenv("CLAUDE_SESSION_ID", "claude-sess-99")
	if id := ExtractSessionID(p); id != "claude-sess-99" {
		t.Errorf("expected env priority claude-sess-99, got %s", id)
	}
	t.Setenv("CODEX_SESSION_ID", "codex-thread-888")
	if id := ExtractSessionID(p); id != "codex-thread-888" {
		t.Errorf("expected Codex session ID codex-thread-888, got %s", id)
	}
}

func TestMapHookEvent(t *testing.T) {
	empty := Payload{}
	if got := MapHookEvent("stop", "", empty); got != "agentCompletion" {
		t.Errorf("Cursor stop completed = %q", got)
	}
	if got := MapHookEvent("stop", "", Payload{Status: "aborted"}); got != "failed" {
		t.Errorf("Cursor stop aborted = %q", got)
	}
	if got := MapHookEvent("sessionEnd", "", Payload{Reason: "error"}); got != "failed" {
		t.Errorf("sessionEnd error = %q", got)
	}
	if got := MapHookEvent("preToolUse", "Shell", Payload{Command: ""}); got != "beforeShellExecution" {
		t.Errorf("preToolUse Shell = %q", got)
	}
	if got := MapHookEvent("preToolUse", "Task", empty); got != "toolUse" {
		t.Errorf("preToolUse Task = %q", got)
	}
	if got := MapHookEvent("beforeMCPExecution", "search", empty); got != "toolUse" {
		t.Errorf("beforeMCPExecution = %q", got)
	}
	if got := MapHookEvent("postToolUseFailure", "Shell", empty); got != "toolFailure" {
		t.Errorf("postToolUseFailure = %q", got)
	}
	if got := MapHookEvent("afterAgentResponse", "", empty); got != "afterAgentResponse" {
		t.Errorf("afterAgentResponse = %q", got)
	}
}

func TestExtractAIResponseFromCursorTranscript(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sess.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>分析适配</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"先看 Hook 协议。"},{"type":"tool_use","name":"Grep"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExtractAIResponseFromTranscripts(Payload{TranscriptPath: path}, "sess")
	if got != "先看 Hook 协议。" {
		t.Errorf("AI response = %q", got)
	}
	turns, curr, _ := ExtractTurnInfo(Payload{TranscriptPath: path}, "sess", 0)
	if turns != 1 || curr != "分析适配" {
		t.Errorf("turn/prompt = %d %q", turns, curr)
	}
}

func TestExtractTurnInfo_DoesNotInflateWithAttachmentsOrDuplicatePrompt(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sess.jsonl"
	content := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>分析适配</user_query>"}]}}
{"role":"user","message":{"content":[{"type":"text","text":"<attached_files>\nfoo.go\n</attached_files>"}]}}
{"role":"user","message":{"content":[{"type":"text","text":"<user_query>分析适配</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"先看 Hook 协议。"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	turns, curr, _ := ExtractTurnInfo(Payload{
		TranscriptPath: path,
		Prompt:         "<user_query>分析适配</user_query>",
	}, "sess", 0)
	if turns != 1 {
		t.Errorf("turns = %d; want 1 (attachments and duplicate payload must not inflate)", turns)
	}
	if curr != "分析适配" {
		t.Errorf("current prompt = %q", curr)
	}
}

func TestDeliverEvent_SpoolsWhenMonitorDownThenReplays(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	t.Setenv("AGENT_REPORTER_SPOOL", spool)

	down := EventReport{ID: "sess-spool", Event: "afterAgentResponse", Detail: "AI 回复: done", Timestamp: 1}
	DeliverEvent("http://127.0.0.1:1", down)
	raw, err := os.ReadFile(spool)
	if err != nil {
		t.Fatalf("expected spool file: %v", err)
	}
	if !strings.Contains(string(raw), "afterAgentResponse") {
		t.Fatalf("spool = %s", raw)
	}

	var got []EventReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report EventReport
		json.NewDecoder(r.Body).Decode(&report)
		got = append(got, report)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	next := EventReport{ID: "sess-spool", Event: "sessionStart", Detail: "用户提交 Prompt", Timestamp: 2}
	DeliverEvent(server.URL, next)

	if len(got) != 2 {
		t.Fatalf("replayed %d events, want 2 (spooled completion then current)", len(got))
	}
	if got[0].Event != "afterAgentResponse" || got[1].Event != "sessionStart" {
		t.Fatalf("order = %s then %s", got[0].Event, got[1].Event)
	}
		if _, err := os.Stat(spool); !os.IsNotExist(err) {
			t.Fatalf("spool should be drained, stat err=%v", err)
		}
	}

	func TestSendEventWithAction_ControlInversion(t *testing.T) {
		// 模拟 Monitor 返回 deny 指令
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","action":"deny","reason":"Manually aborted from dashboard"}`))
		}))
		defer server.Close()

		report := EventReport{
			ID:        "sess-control-test",
			Agent:     "Cursor Agent",
			Event:     "preToolUse",
			Timestamp: time.Now().Unix(),
		}

		ok, ctrl := SendEventWithAction(server.URL, "", report)
		if !ok {
			t.Fatalf("expected send to succeed")
		}
		if ctrl.Action != "deny" {
			t.Fatalf("expected ctrl.Action 'deny', got %q", ctrl.Action)
		}
		if ctrl.Reason != "Manually aborted from dashboard" {
			t.Fatalf("expected ctrl.Reason 'Manually aborted from dashboard', got %q", ctrl.Reason)
		}
	}
