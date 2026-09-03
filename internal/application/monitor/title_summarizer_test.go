package monitor

import (
	"encoding/json"
	"fmt"
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

func TestParseGoalEveryN(t *testing.T) {
	cases := map[string]int{
		"":    3,
		"  ":  3,
		"abc": 3,
		"-1":  3,
		"0":   0,
		"1":   1,
		"3":   3,
		"20":  20,
		"21":  20,
		" 2 ": 2,
	}
	for in, want := range cases {
		if got := parseGoalEveryN(in); got != want {
			t.Errorf("parseGoalEveryN(%q)=%d want %d", in, got, want)
		}
	}
}

func TestShouldRefreshGoal(t *testing.T) {
	mk := func(runs int, lastStatus string, goalAt int) *task.Task {
		tk := &task.Task{TotalRuns: runs, GoalSummaryRun: goalAt, Runs: make([]task.Turn, runs)}
		for i := 0; i < runs; i++ {
			st := "completed"
			if i == runs-1 {
				st = lastStatus
			}
			tk.Runs[i] = task.Turn{Index: i + 1, Status: st}
		}
		return tk
	}
	if !shouldRefreshGoal(mk(3, "completed", 0), 3, "afterAgentResponse") {
		t.Fatal("run 3 should refresh")
	}
	if shouldRefreshGoal(mk(2, "completed", 0), 3, "afterAgentResponse") {
		t.Fatal("run 2 must not refresh when N=3")
	}
	if shouldRefreshGoal(mk(4, "completed", 3), 3, "afterAgentResponse") {
		t.Fatal("run 4 remainder must wait for normalized session terminal, not afterAgentResponse")
	}
	if !shouldRefreshGoal(mk(4, "completed", 3), 3, "agentCompletion") {
		t.Fatal("reporter-normalized stop/sessionEnd (agentCompletion) should refresh remainder")
	}
	if !shouldRefreshGoal(mk(4, "failed", 3), 3, "failed") {
		t.Fatal("fatal terminal failed should refresh remainder")
	}
	if !shouldRefreshGoal(mk(5, "completed", 3), 3, "sessionEnd") {
		t.Fatal("sessionEnd remainder should refresh")
	}
	if shouldRefreshGoal(mk(3, "completed", 0), 0, "afterAgentResponse") {
		t.Fatal("N=0 disables goal")
	}
	if !shouldRefreshGoal(mk(2, "completed", 0), 2, "stop") {
		t.Fatal("N=2 should refresh on run 2")
	}
	if shouldRefreshGoal(mk(4, "running", 3), 3, "afterAgentResponse") {
		t.Fatal("already summarized settled=3")
	}
	if !shouldRefreshGoal(mk(4, "running", 0), 3, "afterAgentResponse") {
		t.Fatal("new run started after 3 should still summarize settled=3")
	}
}

func llmTestServer(t *testing.T, hits *atomic.Int32, goalHits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req chatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad request json: %v", err)
		}
		content := "支付模块短标题"
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "overall session goal") {
			goalHits.Add(1)
			content = "跨轮完成支付模块拆分，并补齐接口测试。"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
}

func settlePrompt(t *testing.T, svc *MonitorService, id, prompt, event string) {
	t.Helper()
	if _, err := svc.HandleHookEvent(task.EventPayload{
		ID:        id,
		Agent:     "ZCode",
		Event:     "UserPromptSubmit",
		Prompt:    prompt,
		Timestamp: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.HandleHookEvent(task.EventPayload{
		ID:         id,
		Event:      event,
		AIResponse: "done",
		Timestamp:  time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
}

func waitLLMTitle(t *testing.T, svc *MonitorService, id string) *task.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := svc.GetTask(id)
		if got != nil && got.TitleSource == "llm" {
			return got
		}
		time.Sleep(15 * time.Millisecond)
	}
	got := svc.GetTask(id)
	t.Fatalf("timeout waiting LLM title, got %+v", got)
	return got
}

func TestGoalSummarizer_EveryThreeRuns(t *testing.T) {
	var hits, goalHits atomic.Int32
	srv := llmTestServer(t, &hits, &goalHits)
	t.Cleanup(srv.Close)

	svc := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, nil)
	t.Cleanup(svc.Close)
	sum := NewTitleSummarizer(srv.URL, "", "test-model", 2*time.Second)
	svc.SetTitleSummarizer(sum)

	id := "sess-goal-n3"
	root := "#task 拆支付模块"
	settlePrompt(t, svc, id, root, "afterAgentResponse")
	waitLLMTitle(t, svc, id)
	if goalHits.Load() != 0 {
		t.Fatalf("run 1 must not call goal, got %d", goalHits.Load())
	}

	settlePrompt(t, svc, id, "补仓储实现", "afterAgentResponse")
	waitLLMTitle(t, svc, id)
	if goalHits.Load() != 0 {
		t.Fatalf("run 2 must not call goal, got %d", goalHits.Load())
	}

	settlePrompt(t, svc, id, "写接口测试", "afterAgentResponse")
	deadline := time.Now().Add(2 * time.Second)
	var got *task.Task
	for time.Now().Before(deadline) {
		got = svc.GetTask(id)
		if got != nil && got.GoalSummary != "" && got.GoalSummaryRun == 3 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if got == nil || got.GoalSummary != "跨轮完成支付模块拆分，并补齐接口测试。" || got.GoalSummaryRun != 3 {
		t.Fatalf("expected goal on run 3, got summary=%q run=%d", got.GoalSummary, got.GoalSummaryRun)
	}
	if got.RootGoal != root {
		t.Fatalf("RootGoal rewritten: %q", got.RootGoal)
	}
	if goalHits.Load() != 1 {
		t.Fatalf("goal hits = %d want 1", goalHits.Load())
	}

	settlePrompt(t, svc, id, "修 flaky 测试", "afterAgentResponse")
	time.Sleep(80 * time.Millisecond)
	if goalHits.Load() != 1 {
		t.Fatalf("run 4 must not call goal again, got %d", goalHits.Load())
	}

	if _, err := svc.HandleHookEvent(task.EventPayload{
		ID:        id,
		Event:     "agentCompletion",
		Timestamp: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got = svc.GetTask(id)
		if got != nil && got.GoalSummaryRun == 4 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if got.GoalSummaryRun != 4 {
		t.Fatalf("agentCompletion remainder GoalSummaryRun=%d", got.GoalSummaryRun)
	}
	if got.RootGoal != root {
		t.Fatalf("RootGoal rewritten after agentCompletion: %q", got.RootGoal)
	}
	if goalHits.Load() != 2 {
		t.Fatalf("agentCompletion remainder goal hits = %d want 2", goalHits.Load())
	}
}

func TestGoalSummarizer_CustomNAndDisabled(t *testing.T) {
	var hits, goalHits atomic.Int32
	srv := llmTestServer(t, &hits, &goalHits)
	t.Cleanup(srv.Close)

	svc := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, nil)
	t.Cleanup(svc.Close)
	sum := NewTitleSummarizer(srv.URL, "", "test-model", 2*time.Second)
	sum.SetGoalEveryN(2)
	svc.SetTitleSummarizer(sum)

	id := "sess-goal-n2"
	settlePrompt(t, svc, id, "#task 自定义间隔", "afterAgentResponse")
	waitLLMTitle(t, svc, id)
	if goalHits.Load() != 0 {
		t.Fatalf("N=2 run 1 must not goal, got %d", goalHits.Load())
	}
	settlePrompt(t, svc, id, "第二轮", "afterAgentResponse")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := svc.GetTask(id)
		if got != nil && got.GoalSummaryRun == 2 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	got := svc.GetTask(id)
	if got.GoalSummaryRun != 2 {
		t.Fatalf("N=2 expected goal on run 2, run=%d", got.GoalSummaryRun)
	}

	var disabledHits, disabledGoal atomic.Int32
	srv2 := llmTestServer(t, &disabledHits, &disabledGoal)
	t.Cleanup(srv2.Close)
	svc2 := NewMonitorService(&memoryRepo{tasks: make(map[string]*task.Task)}, nil)
	t.Cleanup(svc2.Close)
	sum2 := NewTitleSummarizer(srv2.URL, "", "test-model", 2*time.Second)
	sum2.SetGoalEveryN(0)
	svc2.SetTitleSummarizer(sum2)
	settlePrompt(t, svc2, "sess-goal-off", "#task 关闭总目标", "afterAgentResponse")
	waitLLMTitle(t, svc2, "sess-goal-off")
	settlePrompt(t, svc2, "sess-goal-off", "第二轮", "afterAgentResponse")
	waitLLMTitle(t, svc2, "sess-goal-off")
	time.Sleep(80 * time.Millisecond)
	off := svc2.GetTask("sess-goal-off")
	if disabledGoal.Load() != 0 || off.GoalSummary != "" {
		t.Fatalf("N=0 must never summarize goal, hits=%d summary=%q", disabledGoal.Load(), off.GoalSummary)
	}
	if off.TitleSource != "llm" {
		t.Fatal("N=0 must still summarize titles")
	}
}
