package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Zelayan/agent-monitor/internal/domain/task"
)

const (
	titleSummaryTimeout = 4 * time.Second
	titlePromptMaxRunes = 200
	titleReplyMaxRunes  = 160
	titleDigestMaxRuns  = 12
)

const titleSummarySystemPrompt = `You summarize a multi-turn AI coding session into a short display title.
Reply with only the title: at most 24 Chinese characters or 12 English words.
No labels, no quotes, no #task, no markdown, no trailing punctuation.`

// TitleSummarizer 通过 OpenAI 兼容 Chat Completions 为会话容器生成短标题。
// 未配置 BASE_URL / MODEL 时 Disabled，调用方不得发网。
type TitleSummarizer struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

type titleJobState struct {
	mu      sync.Mutex
	running bool
	pending bool
}

// NewTitleSummarizerFromEnv 读取 AGENT_MONITOR_LLM_* 环境变量；未配齐则返回 nil。
func NewTitleSummarizerFromEnv() *TitleSummarizer {
	return NewTitleSummarizer(
		os.Getenv("AGENT_MONITOR_LLM_BASE_URL"),
		os.Getenv("AGENT_MONITOR_LLM_API_KEY"),
		os.Getenv("AGENT_MONITOR_LLM_MODEL"),
		titleSummaryTimeout,
	)
}

// NewTitleSummarizer 构造可注入 HTTP 超时的 summarizer；URL 或 Model 为空时返回 nil。
func NewTitleSummarizer(baseURL, apiKey, model string, timeout time.Duration) *TitleSummarizer {
	baseURL = strings.TrimSpace(baseURL)
	model = strings.TrimSpace(model)
	if baseURL == "" || model == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = titleSummaryTimeout
	}
	return &TitleSummarizer{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Enabled 表示当前实例可发请求。
func (s *TitleSummarizer) Enabled() bool {
	return s != nil && s.baseURL != "" && s.model != "" && s.httpClient != nil
}

type chatCompletionRequest struct {
	Model       string              `json:"model"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
	Messages    []chatCompletionMsg `json:"messages"`
}

type chatCompletionMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Summarize 根据会话各轮 Prompt / 回复生成短标题。失败返回 error，不得回写空标题。
func (s *TitleSummarizer) Summarize(snapshot *task.Task) (string, error) {
	if !s.Enabled() {
		return "", fmt.Errorf("title summarizer disabled")
	}
	if snapshot == nil {
		return "", fmt.Errorf("nil task snapshot")
	}
	digest := buildTitleDigest(snapshot)
	if strings.TrimSpace(digest) == "" {
		return "", fmt.Errorf("empty session digest")
	}

	payload, err := json.Marshal(chatCompletionRequest{
		Model:       s.model,
		Temperature: 0.2,
		MaxTokens:   64,
		Messages: []chatCompletionMsg{
			{Role: "system", Content: titleSummarySystemPrompt},
			{Role: "user", Content: digest},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, chatCompletionsURL(s.baseURL), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm http %d", resp.StatusCode)
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm empty choices")
	}
	title := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if title == "" {
		return "", fmt.Errorf("llm empty content")
	}
	return title, nil
}

func chatCompletionsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	switch {
	case strings.HasSuffix(base, "/chat/completions"):
		return base
	case strings.HasSuffix(base, "/v1"):
		return base + "/chat/completions"
	default:
		return base + "/v1/chat/completions"
	}
}

func buildTitleDigest(t *task.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Agent: %s\n", strings.TrimSpace(t.Agent))
	if t.RootGoal != "" {
		fmt.Fprintf(&b, "RootGoal: %s\n", truncateRunes(t.RootGoal, titlePromptMaxRunes))
	}
	runs := t.Runs
	if len(runs) == 0 {
		runs = t.Turns
	}
	if len(runs) > titleDigestMaxRuns {
		runs = runs[len(runs)-titleDigestMaxRuns:]
	}
	for _, run := range runs {
		fmt.Fprintf(&b, "\nRun #%d [%s]\n", run.Index, run.Status)
		if run.Title != "" {
			fmt.Fprintf(&b, "Title: %s\n", truncateRunes(run.Title, 80))
		}
		if run.Prompt != "" {
			fmt.Fprintf(&b, "Prompt: %s\n", truncateRunes(run.Prompt, titlePromptMaxRunes))
		}
		if run.AIResponse != "" {
			fmt.Fprintf(&b, "Reply: %s\n", truncateRunes(run.AIResponse, titleReplyMaxRunes))
		}
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func isTurnSettleEvent(event string) bool {
	switch event {
	case "agentCompletion", "onComplete", "complete", "Stop", "stop", "SessionEnd", "sessionEnd", "afterAgentResponse", "failed", "error":
		return true
	default:
		return false
	}
}
