package reporter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// IgnoredTools 忽略微粒度文件修改与纯读取/管理工具，避免污染监控时间线
var IgnoredTools = map[string]bool{
	"Read":               true,
	"read":               true,
	"read_file":          true,
	"Glob":               true,
	"glob":               true,
	"Grep":               true,
	"grep":               true,
	"grep_search":        true,
	"file_search":        true,
	"TodoWrite":          true,
	"TodoRead":           true,
	"WebFetch":           true,
	"WebSearch":          true,
	"ReadSessionContext": true,
	"AskUserQuestion":    true,
	"SendMessage":        true,
	"TaskOutput":         true,
	"TaskStop":           true,
	"CronCreate":         true,
	"CronDelete":         true,
	"CronList":           true,
	"CronUpdate":         true,
	"EnterPlanMode":      true,
	"ExitPlanMode":       true,
	"Skill":              true,
	"Edit":               true,
	"Write":              true,
	"edit_file":          true,
	"write_file":         true,
	"ApplyPatch":         true,
}

const (
	maxAIResponseBytes = 64 * 1024
	spoolFlushLimit    = 16
	maxSpoolFileBytes  = 2 << 20
)

// Config 封装 CLI 传入参数与运行配置
type Config struct {
	Event      string
	Agent      string
	Turn       int
	ServerURL  string
	RequireTag string
}

// Payload 定义 Hook 传入的 JSON 结构体（兼容多种 Agent 的字段名）
type Payload struct {
	Raw            string                 `json:"raw,omitempty"`
	ID             string                 `json:"id,omitempty"`
	TaskID         string                 `json:"task_id,omitempty"`
	TaskIDCamel    string                 `json:"taskId,omitempty"`
	Agent          string                 `json:"agent,omitempty"`
	HookEventName  string                 `json:"hook_event_name,omitempty"`
	HookName       string                 `json:"hook_name,omitempty"`
	Event          string                 `json:"event,omitempty"`
	SessionID      string                 `json:"session_id,omitempty"`
	SessionIDCamel string                 `json:"sessionId,omitempty"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	GenerationID   string                 `json:"generation_id,omitempty"`
	ToolName       string                 `json:"tool_name,omitempty"`
	Tool           string                 `json:"tool,omitempty"`
	ToolInput      json.RawMessage        `json:"tool_input,omitempty"` // Cursor: object 或 MCP JSON 字符串
	ToolArgs       map[string]interface{} `json:"tool_args,omitempty"`
	Parameters     map[string]interface{} `json:"parameters,omitempty"`
	Command        string                 `json:"command,omitempty"`
	Cwd            string                 `json:"cwd,omitempty"`
	Prompt         interface{}            `json:"prompt,omitempty"`
	UserPrompt     interface{}            `json:"user_prompt,omitempty"`
	UserQuery      interface{}            `json:"user_query,omitempty"`
	UserMessage    interface{}            `json:"user_message,omitempty"`
	Task           interface{}            `json:"task,omitempty"`
	Input          interface{}            `json:"input,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	Message        string                 `json:"message,omitempty"`
	Status         string                 `json:"status,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	Text           string                 `json:"text,omitempty"`
	FilePath       string                 `json:"file_path,omitempty"`
	MCPServerName  string                 `json:"mcp_server_name,omitempty"`
	WorkspaceRoots []string               `json:"workspace_roots,omitempty"`
	TranscriptPath string                 `json:"transcript_path,omitempty"`
}

// DetectAgentName 智能推断上报来源的 Agent 名称
func DetectAgentName(cfgAgent, payloadAgent string) string {
	if cfgAgent != "" {
		return cfgAgent
	}
	if payloadAgent != "" {
		return payloadAgent
	}
	if envAgent := os.Getenv("AGENT_NAME"); envAgent != "" {
		return envAgent
	}

	execCmd := strings.ToLower(os.Getenv("_"))

	// 1. Claude Code
	if os.Getenv("CLAUDE_SESSION_ID") != "" || os.Getenv("CLAUDE_PROJECT_DIR") != "" || strings.Contains(execCmd, "claude") {
		return "Claude Code"
	}
	// 2. ZCode
	if os.Getenv("ZCODE_SESSION_ID") != "" || os.Getenv("ZCODE_PROJECT_DIR") != "" || strings.Contains(execCmd, "zcode") {
		return "ZCode"
	}
	// 3. Aider
	if os.Getenv("AIDER_SESSION_ID") != "" || strings.Contains(execCmd, "aider") {
		return "Aider"
	}
	// 4. Trae
	if os.Getenv("TRAE_PROJECT_DIR") != "" || os.Getenv("TRAE_SESSION_ID") != "" {
		return "Trae"
	}
	// 5. Windsurf / Codeium Cascade
	if os.Getenv("WINDSURF_PROJECT_DIR") != "" || os.Getenv("CODEIUM_PROJECT_DIR") != "" {
		return "Windsurf"
	}
	// 6. Continue
	if os.Getenv("CONTINUE_SESSION_ID") != "" || os.Getenv("CONTINUE_GLOBAL_DIR") != "" {
		return "Continue"
	}
	// 7. Cursor
	if os.Getenv("CURSOR_PROJECT_DIR") != "" || os.Getenv("CURSOR_TRANSCRIPT_PATH") != "" || strings.Contains(execCmd, "cursor") {
		return "Cursor Agent"
	}

	return "AI Agent"
}

// ExtractSessionID 提取唯一的会话标识
func ExtractSessionID(payload Payload) string {
	for _, envK := range []string{
		"ZCODE_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"CURSOR_SESSION_ID",
		"AIDER_SESSION_ID",
		"TRAE_SESSION_ID",
		"CONTINUE_SESSION_ID",
		"AGENT_SESSION_ID",
	} {
		if v := os.Getenv(envK); v != "" {
			return v
		}
	}

	for _, v := range []string{
		payload.ConversationID, // Cursor 跨轮稳定 ID，优先于 generation_id
		payload.SessionID,
		payload.SessionIDCamel,
		payload.ID,
		payload.TaskID,
		payload.TaskIDCamel,
		payload.GenerationID,
	} {
		if v != "" {
			return v
		}
	}

	return fmt.Sprintf("sess-%d", time.Now().Unix())
}

// EventReport 向 Monitor 服务端 /api/event 提交的数据结构
type EventReport struct {
	ID         string `json:"id"`
	Agent      string `json:"agent"`
	Repo       string `json:"repo"`
	Event      string `json:"event"`
	Timestamp  int64  `json:"timestamp"`
	Detail     string `json:"detail"`
	TurnIndex  int    `json:"turn_index"`
	Title      string `json:"title,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	AIResponse string `json:"ai_response,omitempty"`
}

// GetHookResponse 根据事件名返回对应的 Hook 协议 JSON 响应
func GetHookResponse(event string) string {
	switch event {
	case "beforeSubmitPrompt", "UserPromptSubmit":
		return `{"continue":true}`
	case "beforeShellExecution", "beforeMCPExecution", "preToolUse", "PreToolUse", "PermissionRequest", "subagentStart":
		return `{"permission":"allow"}`
	default:
		return `{}`
	}
}

// IsFailureEvent 判断是否为单次工具失败（非整会话失败）。
func IsFailureEvent(eventName string) bool {
	switch eventName {
	case "PostToolUseFailure", "postToolUseFailure", "toolFailure":
		return true
	default:
		return false
	}
}

// IsFatalTerminal 根据 Cursor stop/sessionEnd 的 status/reason 判断是否为会话级中断。
func IsFatalTerminal(payload Payload) bool {
	for _, v := range []string{payload.Status, payload.Reason} {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "error", "aborted":
			return true
		}
	}
	return false
}

// MapHookEvent 将各 Agent 原始 Hook 名规范化为 Monitor 状态机事件。
func MapHookEvent(eventName, toolName string, payload Payload) string {
	switch eventName {
	case "beforeSubmitPrompt", "sessionStart", "SessionStart", "UserPromptSubmit":
		return "sessionStart"
	case "stop", "sessionEnd", "agentCompletion", "SessionEnd", "Stop":
		if IsFatalTerminal(payload) {
			return "failed"
		}
		return "agentCompletion"
	case "PostToolUseFailure", "postToolUseFailure":
		return "toolFailure"
	case "error", "failed":
		return "failed"
	case "beforeMCPExecution", "afterMCPExecution", "subagentStart", "subagentStop":
		return "toolUse"
	case "PreToolUse", "preToolUse", "beforeShellExecution":
		if isBashTool(toolName) || payload.Command != "" {
			return "beforeShellExecution"
		}
		return "toolUse"
	case "afterAgentResponse":
		return "afterAgentResponse"
	default:
		return eventName
	}
}

func shouldDropEvent(eventName string) bool {
	switch eventName {
	case "afterFileEdit", "afterTabFileEdit", "beforeReadFile", "beforeTabFileRead", "afterAgentThought", "preCompact", "workspaceOpen":
		return true
	default:
		return false
	}
}

func isPreOrPostToolUse(eventName string) bool {
	switch eventName {
	case "PreToolUse", "PostToolUse", "preToolUse", "postToolUse":
		return true
	default:
		return false
	}
}

// RespondAndExit 打印 Hook 协议响应并以退出码 0 退出
func RespondAndExit(event string) {
	fmt.Println(GetHookResponse(event))
	os.Exit(0)
}

func sessionTrackedFile(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	cleanID := strings.ReplaceAll(sessionID, "/", "_")
	cleanID = strings.ReplaceAll(cleanID, "\\", "_")
	return filepath.Join(os.TempDir(), fmt.Sprintf("agent-monitor-tracked-%s", cleanID))
}

func isSessionTracked(sessionID string) bool {
	p := sessionTrackedFile(sessionID)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func markSessionTracked(sessionID string) {
	p := sessionTrackedFile(sessionID)
	if p != "" {
		_ = os.WriteFile(p, []byte("1"), 0644)
	}
}

func unmarkSessionTracked(sessionID string) {
	p := sessionTrackedFile(sessionID)
	if p != "" {
		_ = os.Remove(p)
	}
}

// Run 执行完整的 Reporter 逻辑
func Run(cfg Config, inputReader io.Reader) {
	payload := parsePayload(inputReader)

	// 0. 解析工作区根目录，加载分层配置：全局 ~/.agent-monitor/config.json <- 项目级 .agent-monitor.json <- 环境变量
	workspaceRoot := ResolveWorkspaceRoot(payload)
	effectiveCfg := LoadConfigForWorkspace(workspaceRoot)
	if effectiveCfg.Disabled {
		RespondAndExit(cfg.Event)
		return
	}

	if cfg.ServerURL == "" {
		if effectiveCfg.ServerURL != "" {
			cfg.ServerURL = effectiveCfg.ServerURL
		} else {
			cfg.ServerURL = "http://127.0.0.1:8000/api/event"
		}
	}

	// 命令行显式传入的参数具有最高优先级
	if cfg.RequireTag != "" {
		effectiveCfg.RequireTag = cfg.RequireTag
	}

	// 1. 确定 Agent 名称（支持全主流 Agent 环境推导）
	agentName := DetectAgentName(cfg.Agent, payload.Agent)

	// 2. 确定 Hook 事件名
	eventName := cfg.Event
	if eventName == "" {
		eventName = payload.HookEventName
	}
	if eventName == "" {
		eventName = payload.HookName
	}
	if eventName == "" {
		eventName = payload.Event
	}
	if eventName == "" {
		eventName = "unknown"
	}

	// 3. 获取 Session ID
	sessionID := ExtractSessionID(payload)

	// 4. 事件过滤
	toolName := payload.ToolName
	if toolName == "" {
		toolName = payload.Tool
	}

	isFailure := IsFailureEvent(eventName)

	if shouldDropEvent(eventName) {
		flushSpool(cfg.ServerURL, spoolFlushLimit)
		RespondAndExit(eventName)
		return
	}

	if isPreOrPostToolUse(eventName) {
		if !isFailure {
			if IgnoredTools[toolName] {
				flushSpool(cfg.ServerURL, spoolFlushLimit)
				RespondAndExit(eventName)
				return
			}
			if eventName == "PostToolUse" || eventName == "postToolUse" {
				flushSpool(cfg.ServerURL, spoolFlushLimit)
				RespondAndExit(eventName)
				return
			}
			// Cursor 的 Shell/MCP 另有 beforeShellExecution / beforeMCPExecution，避免预工具钩子重复上报。
			// Claude Code 使用 PascalCase PreToolUse，不受此分支影响。
			if eventName == "preToolUse" && (isBashTool(toolName) || payload.MCPServerName != "" || strings.HasPrefix(toolName, "MCP:")) {
				flushSpool(cfg.ServerURL, spoolFlushLimit)
				RespondAndExit(eventName)
				return
			}
		}
	}

		// 5. 提取多轮 Prompt 与标题
		turnCount, currentPrompt, firstPrompt := ExtractTurnInfo(payload, sessionID, cfg.Turn)
		title := ShortTitle(currentPrompt)
		if title == "" {
			title = ShortTitle(firstPrompt)
		}

		// 5.1 标签规则过滤 (如 require_tag: "#task")
		if strings.TrimSpace(effectiveCfg.RequireTag) != "" {
			hasTag := effectiveCfg.MatchesRequireTag(currentPrompt, firstPrompt, title)
			if hasTag {
				markSessionTracked(sessionID)
			} else {
				// 未命中标签，检查此前是否已被激活（多轮历史）
				if !isSessionTracked(sessionID) {
					// 未激活且未命中指定标签（包括刚启动无 prompt 阶段）：静默放行，不打扰监控台
					RespondAndExit(eventName)
					return
				}
			}
		}

	// 6. 动态提取操作细节
	detail := ExtractDetail(payload, eventName, toolName, isFailure)

	// 7. 获取 Git 仓库与分支
	repo, branch := GetGitInfo(payload)

	// 8. 映射事件名称
	mappedEvent := MapHookEvent(eventName, toolName, payload)

	// 9. AI 回复：只在 afterAgentResponse 读取。stop 再扫 transcript 会挤占 800ms POST 预算，
	// 导致完成事件发不出去；正文已由 afterAgentResponse 写入同一 Run。
	aiResponseText := strings.TrimSpace(payload.Text)
	if eventName == "afterAgentResponse" && aiResponseText == "" {
		aiResponseText = ExtractAIResponseFromTranscripts(payload, sessionID)
	}
	if len(aiResponseText) > maxAIResponseBytes {
		aiResponseText = aiResponseText[:maxAIResponseBytes]
	}
	if aiResponseText != "" && eventName == "afterAgentResponse" {
		aiSummary := ShortTitle(aiResponseText)
		if aiSummary != "" {
			detail = fmt.Sprintf("AI 回复: %s", aiSummary)
		}
	}

	if len(detail) > 160 {
		detail = detail[:160]
	}

	data := EventReport{
		ID:        sessionID,
		Agent:     agentName,
		Repo:      fmt.Sprintf("%s:%s", repo, branch),
		Event:     mappedEvent,
		Timestamp: time.Now().Unix(),
		Detail:    detail,
		TurnIndex: turnCount,
		Title:     title,
	}
	if len(currentPrompt) > 4000 {
		data.Prompt = currentPrompt[:4000]
	} else {
		data.Prompt = currentPrompt
	}
	if aiResponseText != "" {
		data.AIResponse = aiResponseText
	}

		// 10. 先补报失败队列，再发当前事件；Monitor 宕机时落盘，绝不阻断 Hook
		DeliverEvent(cfg.ServerURL, data)

		// 10.1 若为会话终止或收口事件，清理临时追踪标记
		if eventName == "Stop" || eventName == "stop" || eventName == "SessionEnd" || eventName == "sessionEnd" {
			unmarkSessionTracked(sessionID)
		}

		// 11. 正常响应 Hook 协议
		RespondAndExit(eventName)
}

func parsePayload(r io.Reader) Payload {
	var payload Payload
	if r == nil {
		return payload
	}
	data, err := io.ReadAll(r)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return payload
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		payload.Raw = string(data)
	}
	return payload
}

func isBashTool(tool string) bool {
	switch strings.ToLower(tool) {
	case "bash", "execute_command", "shell", "run_command", "runterminalcommand":
		return true
	default:
		return false
	}
}

// ExtractDetail 提取工具调用或事件的描述文本
func ExtractDetail(payload Payload, eventName, toolName string, isFailure bool) string {
	if isFailure {
		if payload.Error != "" {
			return payload.Error
		}
		if payload.ErrorMessage != "" {
			return payload.ErrorMessage
		}
		if payload.Message != "" {
			return payload.Message
		}
		if toolName != "" {
			return fmt.Sprintf("工具执行失败: %s", toolName)
		}
		return "执行失败"
	}
	if eventName == "beforeMCPExecution" || eventName == "afterMCPExecution" || payload.MCPServerName != "" {
		name := toolName
		if name == "" {
			name = payload.MCPServerName
		}
		if name != "" {
			return fmt.Sprintf("调用 MCP: %s", name)
		}
		return "调用 MCP 工具"
	}
	if eventName == "subagentStart" || eventName == "subagentStop" {
		if s, ok := payload.Task.(string); ok && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("子代理: %s", ShortTitle(s))
		}
		if eventName == "subagentStop" {
			return "子代理结束"
		}
		return "启动子代理"
	}
	if eventName == "afterAgentResponse" {
		if payload.Text != "" {
			return fmt.Sprintf("AI 回复: %s", ShortTitle(payload.Text))
		}
		return "AI 回复完成"
	}
	if payload.Command != "" {
		return fmt.Sprintf("执行命令: %s", payload.Command)
	}
	if isBashTool(toolName) {
		cmd := extractCommandFromArgs(payload)
		if cmd != "" {
			return fmt.Sprintf("执行命令: %s", cmd)
		}
		return "执行命令"
	}
	switch eventName {
	case "sessionStart", "SessionStart":
		return "会话启动，分析任务中..."
	case "beforeSubmitPrompt", "UserPromptSubmit":
		return "用户提交 Prompt"
	case "stop", "sessionEnd", "agentCompletion", "SessionEnd", "Stop":
		if IsFatalTerminal(payload) {
			if payload.ErrorMessage != "" {
				return payload.ErrorMessage
			}
			if payload.Error != "" {
				return payload.Error
			}
			return "任务异常中断"
		}
		return "任务执行完成"
	}
	if toolName != "" {
		return fmt.Sprintf("调用工具: %s", toolName)
	}
	return ""
}

func toolInputAsMap(payload Payload) map[string]interface{} {
	if len(payload.ToolInput) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(payload.ToolInput, &m); err == nil {
		return m
	}
	var s string
	if err := json.Unmarshal(payload.ToolInput, &s); err == nil && s != "" {
		if json.Unmarshal([]byte(s), &m) == nil {
			return m
		}
	}
	return nil
}

func extractCommandFromArgs(payload Payload) string {
	for _, m := range []map[string]interface{}{toolInputAsMap(payload), payload.ToolArgs, payload.Parameters} {
		if m != nil {
			if v, ok := m["command"].(string); ok && v != "" {
				return v
			}
			if v, ok := m["cmd"].(string); ok && v != "" {
				return v
			}
		}
	}
	return ""
}

var userQueryRe = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)
var timestampRe = regexp.MustCompile(`(?s)<timestamp>.*?</timestamp>`)
var whitespaceRe = regexp.MustCompile(`\s+`)

// UnwrapUserQuery 去掉 transcript 包装，抽出 <user_query> 正文
func UnwrapUserQuery(text string) string {
	if text == "" {
		return ""
	}
	if m := userQueryRe.FindStringSubmatch(text); len(m) > 1 {
		text = m[1]
	}
	text = timestampRe.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// ShortTitle 清洗后取第一行作为短标题
func ShortTitle(text string) string {
	if text == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(text, "#task", "")
	cleaned = strings.ReplaceAll(cleaned, "[board]", "")
	cleaned = strings.ReplaceAll(cleaned, "任务:", "")

	scanner := bufio.NewScanner(strings.NewReader(cleaned))
	for scanner.Scan() {
		line := strings.TrimSpace(whitespaceRe.ReplaceAllString(scanner.Text(), " "))
		if line != "" {
			if len([]rune(line)) > 80 {
				return string([]rune(line)[:80])
			}
			return line
		}
	}
	return ""
}

func extractDirectPrompt(payload Payload) string {
	for _, v := range []interface{}{
		payload.Prompt,
		payload.UserPrompt,
		payload.UserQuery,
		payload.UserMessage,
		payload.Task,
		payload.Input,
	} {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return UnwrapUserQuery(s)
		}
	}
	return ""
}

// ExtractTurnInfo 提取当前轮次序号及对应 Prompt
func ExtractTurnInfo(payload Payload, sessionID string, turnArg int) (int, string, string) {
	directPrompt := extractDirectPrompt(payload)

	var allPrompts []string
	for _, path := range TranscriptCandidates(payload, sessionID) {
		prompts := readUserPrompts(path)
		if len(prompts) > 0 {
			allPrompts = prompts
			break
		}
	}

	if directPrompt != "" && !containsPrompt(allPrompts, directPrompt) {
		allPrompts = append(allPrompts, directPrompt)
	}

	turnCount := len(allPrompts)
	if turnCount < 1 {
		turnCount = 1
	}
	if turnArg > 0 {
		turnCount = turnArg
	}

	currentPrompt := directPrompt
	firstPrompt := directPrompt
	if len(allPrompts) > 0 {
		currentPrompt = allPrompts[len(allPrompts)-1]
		firstPrompt = allPrompts[0]
	}

	return turnCount, currentPrompt, firstPrompt
}

func readUserPrompts(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var prompts []string
	scanner := bufio.NewScanner(f)
	// 增大单个 Token 缓冲区上限至 10MB，防止长行溢出
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		text := textFromTranscriptLine(obj)
		if text != "" && (len(prompts) == 0 || prompts[len(prompts)-1] != text) {
			prompts = append(prompts, text)
		}
	}
	return prompts
}

func textFromTranscriptLine(obj map[string]interface{}) string {
	if role, _ := obj["role"].(string); role != "user" {
		return ""
	}
	raw := contentTextFromObj(obj)
	if !isCountableUserPrompt(raw) {
		return ""
	}
	return UnwrapUserQuery(raw)
}

func containsPrompt(prompts []string, prompt string) bool {
	for _, existing := range prompts {
		if existing == prompt {
			return true
		}
	}
	return false
}

// isCountableUserPrompt 过滤 transcript 里不当作「用户新一轮」的注入消息
//（附件、工作区快照等），避免 turn_index 虚高把仍在 running 的一轮顶掉。
func isCountableUserPrompt(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "<user_query>") {
		return UnwrapUserQuery(raw) != ""
	}
	for _, tag := range []string{
		"<attached_files>",
		"<agent_transcript>",
		"<git_status>",
		"<open_and_recently_viewed_files>",
		"<user_info>",
	} {
		if strings.Contains(raw, tag) {
			return false
		}
	}
	return true
}

func contentTextFromObj(obj map[string]interface{}) string {
	msgObj, ok := obj["message"].(map[string]interface{})
	if !ok {
		msgObj = obj
	}
	var chunks []string
	if contentList, ok := msgObj["content"].([]interface{}); ok {
		for _, c := range contentList {
			if cMap, ok := c.(map[string]interface{}); ok {
				if cMap["type"] == "text" {
					if t, ok := cMap["text"].(string); ok {
						chunks = append(chunks, t)
					}
				}
			} else if s, ok := c.(string); ok {
				chunks = append(chunks, s)
			}
		}
	} else if s, ok := msgObj["content"].(string); ok {
		chunks = append(chunks, s)
	} else if s, ok := obj["content"].(string); ok {
		chunks = append(chunks, s)
	} else if s, ok := obj["text"].(string); ok {
		chunks = append(chunks, s)
	}
	return strings.TrimSpace(strings.Join(chunks, "\n"))
}

// TranscriptCandidates 获取可能存在的 Transcript 文件路径（覆盖 Cursor、Claude Code 等）
func TranscriptCandidates(payload Payload, sessionID string) []string {
	var paths []string
	if payload.TranscriptPath != "" {
		paths = append(paths, payload.TranscriptPath)
	}
	if envP := os.Getenv("CURSOR_TRANSCRIPT_PATH"); envP != "" {
		paths = append(paths, envP)
	}

	var roots []string
	for _, envK := range []string{"CURSOR_PROJECT_DIR", "ZCODE_PROJECT_DIR", "CLAUDE_PROJECT_DIR", "TRAE_PROJECT_DIR", "WINDSURF_PROJECT_DIR"} {
		if p := os.Getenv(envK); p != "" {
			roots = append(roots, p)
		}
	}
	roots = append(roots, payload.WorkspaceRoots...)

	home, err := os.UserHomeDir()
	if err == nil {
		for _, root := range roots {
			if root == "" {
				continue
			}
			slug := strings.ReplaceAll(strings.Trim(root, "/"), "/", "-")
			// Cursor transcripts
			baseCursor := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", sessionID)
			paths = append(paths, filepath.Join(baseCursor, sessionID+".jsonl"))
			paths = append(paths, filepath.Join(baseCursor, sessionID+".txt"))

			// Claude Code transcripts
			baseClaude := filepath.Join(home, ".claude", "projects", slug, "transcripts", sessionID)
			paths = append(paths, filepath.Join(baseClaude, sessionID+".jsonl"))
			paths = append(paths, filepath.Join(root, ".claude", "transcripts", sessionID+".jsonl"))
		}
	}

	seen := make(map[string]bool)
	var out []string
	for _, p := range paths {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// ExtractAIResponseFromTranscripts 从 Transcript 文件倒序查找最新 Assistant 回复
func ExtractAIResponseFromTranscripts(payload Payload, sessionID string) string {
	for _, path := range TranscriptCandidates(payload, sessionID) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var lines []string
		scanner := bufio.NewScanner(f)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		f.Close()

		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				continue
			}
			if role, _ := obj["role"].(string); role == "assistant" {
				if text := contentTextFromObj(obj); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

// 包级全局 HTTP Client，复用连接池，杜绝频繁 TCP 握手
var defaultHTTPClient = &http.Client{
	Timeout: 800 * time.Millisecond,
	Transport: &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	},
}

// ResolveWorkspaceRoot 解析当前执行环境对应的工作区根目录
func ResolveWorkspaceRoot(payload Payload) string {
	if len(payload.WorkspaceRoots) > 0 && payload.WorkspaceRoots[0] != "" {
		return payload.WorkspaceRoots[0]
	}
	if payload.Cwd != "" {
		return payload.Cwd
	}
	for _, envK := range []string{"ZCODE_PROJECT_DIR", "CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR", "TRAE_PROJECT_DIR", "WINDSURF_PROJECT_DIR"} {
		if v := os.Getenv(envK); v != "" {
			return v
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// GetGitInfo 获取 Git 仓库名与分支名（轻量级文件解析优先，超时与兜底保护）
func GetGitInfo(payload Payload) (string, string) {
	cwd := ResolveWorkspaceRoot(payload)

	repo := filepath.Base(cwd)
	if repo == "" || repo == "." || repo == "/" {
		repo = "workspace"
	}
	branch := "main"

	// 1. 尝试直接从 .git/HEAD 极速读取当前分支，避免任何外部进程派生开销 (<0.1ms)
	headPath := filepath.Join(cwd, ".git", "HEAD")
	if headData, err := os.ReadFile(headPath); err == nil {
		headStr := strings.TrimSpace(string(headData))
		if strings.HasPrefix(headStr, "ref: refs/heads/") {
			branch = strings.TrimPrefix(headStr, "ref: refs/heads/")
			return repo, branch
		} else if len(headStr) >= 7 {
			branch = headStr[:7] // Detached HEAD SHA
			return repo, branch
		}
	}

	// 2. 降级通过快速子进程探测
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	cmdRepo := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmdRepo.Dir = cwd
	if out, err := cmdRepo.Output(); err == nil {
		top := strings.TrimSpace(string(out))
		if top != "" {
			repo = filepath.Base(top)
		}
	}

	cmdBranch := exec.CommandContext(ctx, "git", "branch", "--show-current")
	cmdBranch.Dir = cwd
	if out, err := cmdBranch.Output(); err == nil {
		b := strings.TrimSpace(string(out))
		if b != "" {
			branch = b
		}
	}

	return repo, branch
}

// DeliverEvent 先补报本地 spool，再发送当前事件。Monitor 不可达时落盘，保证下次 Hook 能按序补报。
func DeliverEvent(serverURL string, report EventReport) {
	flushSpool(serverURL, spoolFlushLimit)
	if !SendEvent(serverURL, report) {
		enqueueSpool(report)
	}
}

func spoolFile() string {
	if v := strings.TrimSpace(os.Getenv("AGENT_REPORTER_SPOOL")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "agent-reporter-spool.jsonl")
	}
	return filepath.Join(home, ".agent-monitor", "spool.jsonl")
}

func enqueueSpool(report EventReport) {
	data, err := json.Marshal(report)
	if err != nil {
		return
	}
	path := spoolFile()
	if info, err := os.Stat(path); err == nil && info.Size() >= maxSpoolFileBytes {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func flushSpool(serverURL string, limit int) {
	if limit <= 0 {
		limit = spoolFlushLimit
	}
	path := spoolFile()
	sending := path + ".sending"
	if err := os.Rename(path, sending); err != nil {
		return
	}
	raw, err := os.ReadFile(sending)
	if err != nil {
		_ = os.Rename(sending, path)
		return
	}
	_ = os.Remove(sending)

	var pending [][]byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		pending = append(pending, line)
	}

	for i, line := range pending {
		if i >= limit {
			appendSpoolLines(pending[i:])
			return
		}
		var report EventReport
		if err := json.Unmarshal(line, &report); err != nil {
			continue
		}
		if !SendEvent(serverURL, report) {
			appendSpoolLines(pending[i:])
			return
		}
	}
}

func appendSpoolLines(lines [][]byte) {
	if len(lines) == 0 {
		return
	}
	path := spoolFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		_, _ = f.Write(line)
		_, _ = f.Write([]byte("\n"))
	}
}

// SendEvent 向 Monitor 发送单条事件，成功返回 true。失败由调用方 spool，绝不 panic。
func SendEvent(serverURL string, report EventReport) bool {
	data, err := json.Marshal(report)
	if err != nil {
		return false
	}

	req, err := http.NewRequest("POST", serverURL, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
