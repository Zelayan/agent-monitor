package monitor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

func TestNewTitleSummarizer_DisabledWhenUnconfigured(t *testing.T) {
	if NewTitleSummarizer("", "key", "model", time.Second) != nil {
		t.Fatal("empty base URL must return nil")
	}
	if NewTitleSummarizer("http://127.0.0.1:11434/v1", "", "", time.Second) != nil {
		t.Fatal("empty model must return nil")
	}
	if s := NewTitleSummarizerFromEnv(); s != nil {
		t.Skip("AGENT_MONITOR_LLM_* is set in this environment")
	}
}

func TestChatCompletionsURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:11434":                     "http://127.0.0.1:11434/v1/chat/completions",
		"http://127.0.0.1:11434/v1":                  "http://127.0.0.1:11434/v1/chat/completions",
		"http://127.0.0.1:11434/v1/":                 "http://127.0.0.1:11434/v1/chat/completions",
		"https://api.openai.com/v1/chat/completions": "https://api.openai.com/v1/chat/completions",
	}
	for in, want := range cases {
		if got := chatCompletionsURL(in); got != want {
			t.Errorf("chatCompletionsURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTitleSummarizer_HandleHookEventDoesNotCallWhenDisabled(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	svc := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, nil)
	t.Cleanup(svc.Close)
	// summarizer left nil on purpose

	_, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-no-llm",
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    "#task 实现支付模块",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-no-llm",
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("disabled summarizer must not hit HTTP, got %d", hits.Load())
	}
	got := svc.GetTask("sess-no-llm")
	if got == nil || got.Title != "实现支付模块" {
		t.Fatalf("expected heuristic title, got %+v", got)
	}
}

func TestTitleSummarizer_SuccessOverwritesTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req chatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad request json: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("model = %q", req.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"\"DDD 重构并补齐测试\""}}]}`))
	}))
	t.Cleanup(srv.Close)

	hub := NewHub()
	go hub.Run()
	svc := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, hub)
	t.Cleanup(svc.Close)
	svc.SetTitleSummarizer(NewTitleSummarizer(srv.URL, "sk-test", "test-model", 2*time.Second))

	_, err := svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-llm-ok",
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    "#task 按照 DDD 重构代码结构\n分成 domain/app/infra",
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.HandleHookEvent(task.EventPayload{
		ID:         "sess-llm-ok",
		Event:      "afterAgentResponse",
		AIResponse: "已拆好分层。",
		Timestamp:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := svc.GetTask("sess-llm-ok")
		if got != nil && got.Title == "DDD 重构并补齐测试" && got.TitleSource == "llm" {
			if got.RootGoal != "#task 按照 DDD 重构代码结构\n分成 domain/app/infra" {
				t.Fatalf("RootGoal rewritten: %q", got.RootGoal)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := svc.GetTask("sess-llm-ok")
	t.Fatalf("expected LLM title, got title=%q source=%q", got.Title, got.TitleSource)
}

func TestTitleSummarizer_TimeoutKeepsHeuristicTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"不该被采用的标题"}}]}`))
	}))
	t.Cleanup(srv.Close)

	svc := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, nil)
	t.Cleanup(svc.Close)
	svc.SetTitleSummarizer(NewTitleSummarizer(srv.URL, "", "slow-model", 50*time.Millisecond))

	prompt := "#task 修复登录超时"
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-llm-timeout",
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    prompt,
		Timestamp: time.Now().Unix(),
	})
	_, _ = svc.HandleHookEvent(task.EventPayload{
		ID:        "sess-llm-timeout",
		Event:     "stop",
		Timestamp: time.Now().Unix(),
	})

	time.Sleep(150 * time.Millisecond)
	got := svc.GetTask("sess-llm-timeout")
	if got == nil {
		t.Fatal("task missing")
	}
	if got.Title != "修复登录超时" {
		t.Fatalf("timeout must keep heuristic title, got %q source=%q", got.Title, got.TitleSource)
	}
	if got.TitleSource == "llm" {
		t.Fatal("timeout must not mark title as llm")
	}
}

func TestBuildTitleDigestIncludesTurns(t *testing.T) {
	digest := buildTitleDigest(&task.Task{
		Agent:    "ZCode",
		RootGoal: "首轮目标",
		Runs: []task.Turn{
			{Index: 1, Title: "重构", Prompt: "按 DDD 重构", AIResponse: "完成", Status: "completed"},
			{Index: 2, Title: "单测", Prompt: "补测试", Status: "running"},
		},
	})
	for _, needle := range []string{"Run #1", "Run #2", "首轮目标", "按 DDD 重构", "补测试"} {
		if !strings.Contains(digest, needle) {
			t.Errorf("digest missing %q:\n%s", needle, digest)
		}
	}
}
