package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestMultiTurnRequireTagSessionTracking(t *testing.T) {
	sessionID := "sess-multiturn-tag-test"
	trackFile := sessionTrackedFile(sessionID)
	promptsFile := sessionPromptsFile(sessionID)
	_ = os.Remove(trackFile)
	_ = os.Remove(promptsFile)
	defer func() {
		_ = os.Remove(trackFile)
		_ = os.Remove(promptsFile)
	}()

	// 1. Turn 1 携带 #task
	p1 := Payload{
		Prompt: "#task 帮我重构代码",
	}
	turn1, curr1, first1 := ExtractTurnInfo(p1, sessionID, 0)
	if turn1 != 1 || curr1 != "#task 帮我重构代码" || first1 != "#task 帮我重构代码" {
		t.Fatalf("unexpected turn 1: turn=%d curr=%q first=%q", turn1, curr1, first1)
	}

	cfg := GlobalConfig{RequireTag: "#task"}
	if !cfg.MatchesRequireTag(curr1, first1, ShortTitle(curr1)) {
		t.Fatalf("turn 1 should match require_tag #task")
	}
	markSessionTracked(sessionID, first1)

	// 验证已经标记
	if !isSessionTracked(sessionID) {
		t.Fatalf("session should be tracked after turn 1")
	}

	// 2. Turn 2 不携带 #task（普通追问）
	p2 := Payload{
		Prompt: "再帮我写一个测试用例",
	}
	turn2, curr2, first2 := ExtractTurnInfo(p2, sessionID, 0)
	if turn2 != 2 {
		t.Fatalf("expected turn 2 count = 2, got %d", turn2)
	}
	if curr2 != "再帮我写一个测试用例" {
		t.Fatalf("expected curr2 to be turn 2 prompt, got %q", curr2)
	}
	if first2 != "#task 帮我重构代码" {
		t.Fatalf("expected first2 to remember first prompt '#task 帮我重构代码', got %q", first2)
	}

	// 验证即使当前轮 Prompt 没有 #task，因为 firstPrompt 有 #task，依然命中标签规则
	if !cfg.MatchesRequireTag(curr2, first2, ShortTitle(curr2)) {
		t.Fatalf("turn 2 should match require_tag because firstPrompt has #task")
	}
	if !isSessionTracked(sessionID) {
		t.Fatalf("session should still be tracked in turn 2")
	}
}

func TestPureGoFindGitRoot(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	_ = os.MkdirAll(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feature-pure-go\n"), 0644)

	subDir := filepath.Join(tempDir, "sub", "pkg", "deep")
	_ = os.MkdirAll(subDir, 0755)

	// 在子目录下探测仓库与分支
	payload := Payload{
		Cwd: subDir,
	}
	repo, branch := GetGitInfo(payload)
	if repo != filepath.Base(tempDir) {
		t.Fatalf("expected repo %q, got %q", filepath.Base(tempDir), repo)
	}
	if branch != "feature-pure-go" {
		t.Fatalf("expected branch feature-pure-go, got %q", branch)
	}
}

func TestCircuitBreakerAndSpoolTruncation(t *testing.T) {
	fakeServer := "http://127.0.0.1:54321/api/event"
	resetCircuitBreaker(fakeServer)
	defer resetCircuitBreaker(fakeServer)

	if isCircuitBreakerOpen(fakeServer) {
		t.Fatalf("circuit breaker should be closed initially")
	}

	tripCircuitBreaker(fakeServer)
	if !isCircuitBreakerOpen(fakeServer) {
		t.Fatalf("circuit breaker should be open after trip")
	}

	// 测试 Spool 截断
	spool := filepath.Join(t.TempDir(), "spool_trunc.jsonl")
	largeData := bytes.Repeat([]byte("{\"id\":\"test\",\"data\":\"1234567890\"}\n"), 100)
	_ = os.WriteFile(spool, largeData, 0644)

	truncateSpoolKeepTail(spool, int64(len(largeData)/2))
	reloaded, _ := os.ReadFile(spool)
	if len(reloaded) >= len(largeData) {
		t.Fatalf("expected truncated file to be smaller than original")
	}
}

func TestShouldSkipUnstartedLifecycle(t *testing.T) {
	if !shouldSkipUnstartedLifecycle("sessionStart", "", "") {
		t.Fatal("empty Cursor sessionStart should be skipped")
	}
	if !shouldSkipUnstartedLifecycle("SessionStart", "", "") {
		t.Fatal("empty SessionStart should be skipped")
	}
	if shouldSkipUnstartedLifecycle("sessionStart", "修一个 bug", "") {
		t.Fatal("sessionStart with current prompt must still be reported")
	}
	if shouldSkipUnstartedLifecycle("sessionStart", "", "首轮问题") {
		t.Fatal("sessionStart with first prompt must still be reported")
	}
	if shouldSkipUnstartedLifecycle("stop", "", "") {
		t.Fatal("empty stop must still be delivered so Monitor can close real work")
	}
	if shouldSkipUnstartedLifecycle("beforeShellExecution", "", "") {
		t.Fatal("tool events must not be skipped as unstarted lifecycle")
	}
}

func TestDeleteSession_And_DroppedSession(t *testing.T) {
	origExit := exitFunc
	exitFunc = func(code int) {}
	defer func() { exitFunc = origExit }()

	sessionID := "test-drop-sess-001"
	unmarkSessionDropped(sessionID)
	unmarkSessionTracked(sessionID)
	defer func() {
		unmarkSessionDropped(sessionID)
		unmarkSessionTracked(sessionID)
	}()

	var receivedMethod, receivedPath, receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"deleted":true}`))
	}))
	defer srv.Close()

	// 1. 验证 DeleteSession HTTP 请求生成与鉴权头
	serverURL := srv.URL + "/api/event"
	ok := DeleteSession(serverURL, "my-test-key", sessionID)
	if !ok {
		t.Fatalf("expected DeleteSession to succeed")
	}
	if receivedMethod != http.MethodDelete {
		t.Fatalf("expected method DELETE, got %s", receivedMethod)
	}
	if receivedPath != "/api/tasks/"+sessionID {
		t.Fatalf("expected path /api/tasks/%s, got %s", sessionID, receivedPath)
	}
	if receivedAuth != "Bearer my-test-key" {
		t.Fatalf("expected Authorization header 'Bearer my-test-key', got %q", receivedAuth)
	}

	// 2. 验证本地 dropped 状态流转
	if isSessionDropped(sessionID) {
		t.Fatalf("session should not be dropped initially")
	}
	markSessionTracked(sessionID, "#task 初始化任务")
	if !isSessionTracked(sessionID) {
		t.Fatalf("session should be tracked")
	}

	markSessionDropped(sessionID)
	unmarkSessionTracked(sessionID)

	if !isSessionDropped(sessionID) {
		t.Fatalf("session should now be dropped")
	}
	if isSessionTracked(sessionID) {
		t.Fatalf("session should no longer be tracked")
	}

	// 3. 验证 Run 中遇到 #drop 关键字自动发起删除并标记 drop
	runSessionID := "test-run-drop-sess-002"
	unmarkSessionDropped(runSessionID)
	unmarkSessionTracked(runSessionID)
	defer func() {
		unmarkSessionDropped(runSessionID)
		unmarkSessionTracked(runSessionID)
	}()

	markSessionTracked(runSessionID, "#task 之前跟踪的任务")

	receivedMethod = ""
	receivedPath = ""
	dropPayload := fmt.Sprintf(`{"session_id":"%s","prompt":"请不要再监控这个会话了 #drop"}`, runSessionID)
	cfg := Config{
		Event:     "beforeSubmitPrompt",
		ServerURL: serverURL,
		DeleteTag: "#drop",
	}

	Run(cfg, strings.NewReader(dropPayload))

	if !isSessionDropped(runSessionID) {
		t.Fatalf("Run with #drop should mark session as dropped")
	}
	if isSessionTracked(runSessionID) {
		t.Fatalf("Run with #drop should unmark session tracked")
	}
	if receivedMethod != http.MethodDelete || receivedPath != "/api/tasks/"+runSessionID {
		t.Fatalf("expected DELETE /api/tasks/%s to be called, got %s %s", runSessionID, receivedMethod, receivedPath)
	}

	// 4. 处于 dropped 状态的后续事件（如工具调用）应该被直接静默忽略，不调用 Server
	callCount := 0
	srvCount := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer srvCount.Close()

	toolPayload := fmt.Sprintf(`{"session_id":"%s","tool_name":"bash","command":"ls"}`, runSessionID)
	toolCfg := Config{
		Event:     "beforeToolUse",
		ServerURL: srvCount.URL + "/api/event",
	}
	Run(toolCfg, strings.NewReader(toolPayload))

	if callCount != 0 {
		t.Fatalf("dropped session events should be silenced and not send requests, got %d calls", callCount)
	}
}

func TestSafeSessionFilename(t *testing.T) {
	// 1. 空字符串防御
	if f := safeSessionFilename("", "prefix", ".json"); f != "" {
		t.Fatalf("expected empty string for empty sessionID, got %q", f)
	}

	// 2. 包含非法 Windows/Unix 字符（冒号、斜杠、反斜杠、Unicode、特殊标点）
	dirtyID := "sess:project/path\\sub:id#123!*?&中文"
	f1 := safeSessionFilename(dirtyID, "test-file", ".json")
	if f1 == "" {
		t.Fatalf("expected non-empty filename for dirtyID")
	}
	base1 := filepath.Base(f1)
	if strings.ContainsAny(base1, `:\/*?<>|"`) {
		t.Fatalf("filename %q contains forbidden filesystem characters", base1)
	}
	if !strings.HasPrefix(base1, "test-file-") || !strings.HasSuffix(base1, ".json") {
		t.Fatalf("unexpected filename format: %s", base1)
	}

	// 3. 超长 SessionID 边界截断防御
	longID := strings.Repeat("very-long-session-identifier-exceeding-standard-filesystem-limit-", 10)
	f2 := safeSessionFilename(longID, "test-long", "")
	base2 := filepath.Base(f2)
	if len(base2) > 100 {
		t.Fatalf("filename %q exceeds safe length bounds: %d", base2, len(base2))
	}

	// 4. 幂等性：同一 sessionID 多次生成必须一致
	f3 := safeSessionFilename(dirtyID, "test-file", ".json")
	if f1 != f3 {
		t.Fatalf("safeSessionFilename must be deterministic, got %q vs %q", f1, f3)
	}
}
