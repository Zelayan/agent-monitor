package reporter

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	DeleteTag  string
	APIKey     string
}

// Payload 定义 Hook 传入的 JSON 结构体（兼容多种 Agent 的字段名）
type Payload struct {
	Raw               string                 `json:"raw,omitempty"`
	ID                string                 `json:"id,omitempty"`
	TaskID            string                 `json:"task_id,omitempty"`
	TaskIDCamel       string                 `json:"taskId,omitempty"`
	ParentID          string                 `json:"parent_id,omitempty"`
	ParentIDCamel     string                 `json:"parentId,omitempty"`
	ParentSessionID   string                 `json:"parent_session_id,omitempty"`
	SubagentID        string                 `json:"subagent_id,omitempty"`
	SubagentIDCamel   string                 `json:"subagentId,omitempty"`
	SubagentType      string                 `json:"subagent_type,omitempty"`
	SubagentTypeCamel string                 `json:"subagentType,omitempty"`
	Agent             string                 `json:"agent,omitempty"`
	HookEventName     string                 `json:"hook_event_name,omitempty"`
	HookName          string                 `json:"hook_name,omitempty"`
	Event             string                 `json:"event,omitempty"`
	SessionID         string                 `json:"session_id,omitempty"`
	SessionIDCamel    string                 `json:"sessionId,omitempty"`
	ConversationID    string                 `json:"conversation_id,omitempty"`
	GenerationID      string                 `json:"generation_id,omitempty"`
	ToolName          string                 `json:"tool_name,omitempty"`
	Tool              string                 `json:"tool,omitempty"`
	ToolInput         json.RawMessage        `json:"tool_input,omitempty"` // Cursor: object 或 MCP JSON 字符串
	ToolArgs          map[string]interface{} `json:"tool_args,omitempty"`
	Parameters        map[string]interface{} `json:"parameters,omitempty"`
	Command           string                 `json:"command,omitempty"`
	Cwd               string                 `json:"cwd,omitempty"`
	Prompt            interface{}            `json:"prompt,omitempty"`
	UserPrompt        interface{}            `json:"user_prompt,omitempty"`
	UserQuery         interface{}            `json:"user_query,omitempty"`
	UserMessage       interface{}            `json:"user_message,omitempty"`
	Task              interface{}            `json:"task,omitempty"`
	Input             interface{}            `json:"input,omitempty"`
	Error             string                 `json:"error,omitempty"`
	ErrorMessage      string                 `json:"error_message,omitempty"`
	Message           string                 `json:"message,omitempty"`
	Status            string                 `json:"status,omitempty"`
	Reason            string                 `json:"reason,omitempty"`
	Text              string                 `json:"text,omitempty"`
	FilePath          string                 `json:"file_path,omitempty"`
	MCPServerName     string                 `json:"mcp_server_name,omitempty"`
	WorkspaceRoots    []string               `json:"workspace_roots,omitempty"`
	TranscriptPath    string                 `json:"transcript_path,omitempty"`
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
	// 8. Codex CLI / Codex Desktop
	if os.Getenv("CODEX_SESSION_ID") != "" || os.Getenv("CODEX_PROJECT_DIR") != "" || os.Getenv("CODEX_WORKSPACE_ROOT") != "" || strings.Contains(execCmd, "codex") {
		if os.Getenv("CODEX_DESKTOP_VERSION") != "" || os.Getenv("CODEX_APP") != "" || strings.Contains(execCmd, "codex desktop") || strings.Contains(execCmd, "codex.app") {
			return "Codex Desktop"
		}
		return "Codex CLI"
	}

	return "AI Agent"
}

// ExtractSessionID 提取唯一的会话标识
func ExtractSessionID(payload Payload) string {
	for _, envK := range []string{
		"CODEX_SESSION_ID",
		"CODEX_THREAD_ID",
		"CODEX_RUN_ID",
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
	ID           string `json:"id"`
	ParentID     string `json:"parent_id,omitempty"`
	SubagentID   string `json:"subagent_id,omitempty"`
	SubagentType string `json:"subagent_type,omitempty"`
	Agent        string `json:"agent"`
	Repo         string `json:"repo"`
	Event        string `json:"event"`
	Timestamp    int64  `json:"timestamp"`
	Detail       string `json:"detail"`
	TurnIndex    int    `json:"turn_index"`
	Title        string `json:"title,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	AIResponse   string `json:"ai_response,omitempty"`
	PID          int    `json:"pid,omitempty"`
	PGID         int    `json:"pgid,omitempty"`
}

// ServerControlResponse 是 Monitor 服务端返回的决策指令。
type ServerControlResponse struct {
	Status            string `json:"status"`           // "ok"
	Action            string `json:"action,omitempty"` // "allow" | "deny" | "abort"
	Reason            string `json:"reason,omitempty"`
	AdditionalContext string `json:"additional_context,omitempty"` // 动态注入上下文
	AgentMessage      string `json:"agent_message,omitempty"`      // 随工具返回给模型的指导
}

// RespondWithAction 根据服务端的控制决策输出阻断协议或默认放行协议，并支持动态上下文注入。
func RespondWithAction(event, agentName string, ctrl ServerControlResponse) {
	if ctrl.Action == "deny" || ctrl.Action == "abort" {
		reason := ctrl.Reason
		if reason == "" {
			reason = "Workflow aborted from Agent Monitor Dashboard"
		}
		agentMsg := ctrl.AgentMessage
		if agentMsg == "" {
			agentMsg = "CRITICAL: The user has intentionally aborted this session. Do not invoke any more tools. Acknowledge and stop immediately."
		}

		switch agentName {
		case "Cursor", "Cursor Agent", "Codex", "Codex CLI", "Codex Desktop":
			if event == "beforeSubmitPrompt" || event == "UserPromptSubmit" {
				out, _ := json.Marshal(map[string]interface{}{
					"continue":     false,
					"user_message": reason,
				})
				fmt.Println(string(out))
			} else {
				out, _ := json.Marshal(map[string]interface{}{
					"permission":    "deny",
					"user_message":  reason,
					"agent_message": agentMsg,
				})
				fmt.Println(string(out))
			}
			os.Exit(0)

		case "ZCode", "Claude Code":
			// ZCode / Claude Code 规范：Exit Code 2 显式阻断
			out, _ := json.Marshal(map[string]interface{}{
				"hookSpecificOutput": map[string]string{
					"permissionDecision": "deny",
				},
				"systemMessage": reason + "\n" + agentMsg,
			})
			fmt.Println(string(out))
			os.Exit(2)

		default:
			// 通用协议：同时包含 permission: deny 与 continue: false
			out, _ := json.Marshal(map[string]interface{}{
				"permission": "deny",
				"continue":   false,
				"message":    reason,
			})
			fmt.Println(string(out))
			os.Exit(2)
		}
		return
	}

	// 放行分支：若有动态上下文注入需求，根据 Hook 协议输出对应的注入字段
	if ctrl.AdditionalContext != "" || ctrl.AgentMessage != "" {
		switch event {
		case "postToolUse", "PostToolUse":
			out, _ := json.Marshal(map[string]interface{}{
				"additional_context": ctrl.AdditionalContext,
			})
			fmt.Println(string(out))
			exitFunc(0)
			return
		case "preToolUse", "PreToolUse", "beforeShellExecution", "beforeMCPExecution", "subagentStart":
			resp := map[string]interface{}{
				"permission": "allow",
			}
			if ctrl.AgentMessage != "" {
				resp["agent_message"] = ctrl.AgentMessage
			}
			if ctrl.AdditionalContext != "" {
				resp["additional_context"] = ctrl.AdditionalContext
			}
			out, _ := json.Marshal(resp)
			fmt.Println(string(out))
			exitFunc(0)
			return
		}
	}

	// 默认安全放行
	RespondAndExit(event)
}

// GetHookResponse 根据事件名返回对应的 Hook 协议 JSON 响应
func GetHookResponse(event string) string {
	switch event {
	case "beforeSubmitPrompt", "UserPromptSubmit":
		return `{"continue":true}`
	case "beforeShellExecution", "beforeMCPExecution", "preToolUse", "PreToolUse", "PermissionRequest", "subagentStart", "SubagentStart":
		return `{"permission":"allow"}`
	default:
		return `{}`
	}
}

// isAgentTool 判断工具是否为子智能体派发工具 (ZCode Agent 等)。
func isAgentTool(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "agent", "spawn_subagent", "subagent":
		return true
	default:
		return false
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
	case "beforeMCPExecution", "afterMCPExecution":
		return "toolUse"
	case "subagentStart", "SubagentStart":
		return "subagentStart"
	case "subagentStop", "SubagentStop":
		return "subagentStop"
	case "PreToolUse", "preToolUse", "beforeShellExecution":
		if isBashTool(toolName) || payload.Command != "" {
			return "beforeShellExecution"
		}
		if isAgentTool(toolName) {
			return "subagentStart"
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

// shouldSkipUnstartedLifecycle 跳过无 Prompt 的会话启动。Cursor 打开 Agent 面板即会触发
// sessionStart；用户什么都不做就关闭时不应向 Monitor 落一条空卡片。
func shouldSkipUnstartedLifecycle(eventName, currentPrompt, firstPrompt string) bool {
	if strings.TrimSpace(currentPrompt) != "" || strings.TrimSpace(firstPrompt) != "" {
		return false
	}
	switch eventName {
	case "sessionStart", "SessionStart":
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

var exitFunc = os.Exit

// RespondAndExit 打印 Hook 协议响应并以退出码 0 退出
func RespondAndExit(event string) {
	fmt.Println(GetHookResponse(event))
	exitFunc(0)
}

func safeSessionFilename(sessionID, prefix, ext string) string {
	if sessionID == "" {
		return ""
	}
	h := sha256.Sum256([]byte(sessionID))
	hashKey := hex.EncodeToString(h[:8]) // 16 字符安全唯一哈希
	// 移除非数字字母下划线字符，保留可读前缀
	var sb strings.Builder
	for _, r := range sessionID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	cleanID := sb.String()
	if len(cleanID) > 32 {
		cleanID = cleanID[:32]
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s-%s%s", prefix, cleanID, hashKey, ext))
}

func sessionTrackedFile(sessionID string) string {
	return safeSessionFilename(sessionID, "agent-monitor-tracked", "")
}

func isSessionTracked(sessionID string) bool {
	p := sessionTrackedFile(sessionID)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func markSessionTracked(sessionID, firstPrompt string) {
	p := sessionTrackedFile(sessionID)
	if p != "" {
		_ = os.WriteFile(p, []byte(firstPrompt), 0644)
	}
}

func getSessionTrackedPrompt(sessionID string) string {
	p := sessionTrackedFile(sessionID)
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarkSessionTracked(sessionID string) {
	p := sessionTrackedFile(sessionID)
	if p != "" {
		_ = os.Remove(p)
	}
}

func sessionDroppedFile(sessionID string) string {
	return safeSessionFilename(sessionID, "agent-monitor-dropped", "")
}

func isSessionDropped(sessionID string) bool {
	p := sessionDroppedFile(sessionID)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func markSessionDropped(sessionID string) {
	p := sessionDroppedFile(sessionID)
	if p != "" {
		_ = os.WriteFile(p, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
	}
}

func unmarkSessionDropped(sessionID string) {
	p := sessionDroppedFile(sessionID)
	if p != "" {
		_ = os.Remove(p)
	}
}

func sessionAbortingFile(sessionID string) string {
	return safeSessionFilename(sessionID, "agent-monitor-aborting", "")
}

func isSessionAborting(sessionID string) bool {
	p := sessionAbortingFile(sessionID)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func markSessionAborting(sessionID string) {
	p := sessionAbortingFile(sessionID)
	if p != "" {
		_ = os.WriteFile(p, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
	}
}

func unmarkSessionAborting(sessionID string) {
	p := sessionAbortingFile(sessionID)
	if p != "" {
		_ = os.Remove(p)
	}
}

func sessionPromptsFile(sessionID string) string {
	return safeSessionFilename(sessionID, "agent-monitor-prompts", ".json")
}

func readSessionPrompts(sessionID string) []string {
	p := sessionPromptsFile(sessionID)
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return nil
	}
	var prompts []string
	if err := json.Unmarshal(data, &prompts); err == nil {
		return prompts
	}
	return nil
}

func recordSessionPrompt(sessionID, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if sessionID == "" || prompt == "" {
		return
	}
	existing := readSessionPrompts(sessionID)
	for _, prev := range existing {
		if prev == prompt {
			return
		}
	}
	existing = append(existing, prompt)
	if data, err := json.Marshal(existing); err == nil {
		p := sessionPromptsFile(sessionID)
		_ = os.WriteFile(p, data, 0644)
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

	if cfg.APIKey == "" && effectiveCfg.APIKey != "" {
		cfg.APIKey = effectiveCfg.APIKey
	}

	// 命令行显式传入的参数具有最高优先级
	if cfg.RequireTag != "" {
		if cfg.RequireTag == "none" || cfg.RequireTag == "all" || cfg.RequireTag == "*" || cfg.RequireTag == `""` {
			effectiveCfg.RequireTag = ""
		} else {
			effectiveCfg.RequireTag = cfg.RequireTag
		}
	}
	if cfg.DeleteTag != "" {
		effectiveCfg.DeleteTag = cfg.DeleteTag
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

	// 若当前会话已被标记为 dropped，任何后续事件（包括 toolUse、stop、afterAgentResponse 等）直接静默放行
	if isSessionDropped(sessionID) {
		RespondAndExit(eventName)
		return
	}

	// 4. 事件过滤
	toolName := payload.ToolName
	if toolName == "" {
		toolName = payload.Tool
	}

	isFailure := IsFailureEvent(eventName)

	if shouldDropEvent(eventName) {
		flushSpoolWithKey(cfg.ServerURL, cfg.APIKey, spoolFlushLimit)
		RespondAndExit(eventName)
		return
	}

	if isPreOrPostToolUse(eventName) {
		if !isFailure {
			// 如果该会话已被标记为中断中 (aborting)，绝不能被 IgnoredTools 静默放行，必须穿透至后端拦截！
			if isSessionAborting(sessionID) {
				// 处于中断中，继续向下交付并阻断
			} else if IgnoredTools[toolName] {
				flushSpoolWithKey(cfg.ServerURL, cfg.APIKey, spoolFlushLimit)
				RespondAndExit(eventName)
				return
			}
			// 对于 PostToolUse，必须上报给服务端以换取可能的动态上下文注入指令（Live Steer）
			// 如果服务端没有待注入内容，会在 DeliverEventWithAction 中得到空的 additional_context 并正常放行退出
			if eventName == "PostToolUse" || eventName == "postToolUse" {
				// 不在此处提前丢弃，继续向下交由服务端处理控制与上下文返回
			}
			// Cursor 的 Shell/MCP 另有 beforeShellExecution / beforeMCPExecution，避免预工具钩子重复上报。
			// Claude Code 使用 PascalCase PreToolUse，不受此分支影响。
			if !isSessionAborting(sessionID) && eventName == "preToolUse" && (isBashTool(toolName) || payload.MCPServerName != "" || strings.HasPrefix(toolName, "MCP:")) {
				flushSpoolWithKey(cfg.ServerURL, cfg.APIKey, spoolFlushLimit)
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

	// 5.-1 会话删除/取消跟踪关键字检测 (如 delete_tag: "#drop,#untrack")
	if strings.TrimSpace(effectiveCfg.DeleteTag) != "" {
		hasDeleteTag := effectiveCfg.MatchesDeleteTag(currentPrompt, title)
		if hasDeleteTag {
			// 用户明确要求丢弃/删除当前会话：
			// 1. 本地标记已丢弃，并清除已追踪标记与历史记录
			markSessionDropped(sessionID)
			unmarkSessionTracked(sessionID)
			// 2. 向 Monitor 发起 DELETE /api/tasks/{sessionID} 回调
			DeleteSession(cfg.ServerURL, cfg.APIKey, sessionID)
			// 3. 静默放行退出
			RespondAndExit(eventName)
			return
		}
	}

	// 检查当前会话是否处于已丢弃黑名单中
	if isSessionDropped(sessionID) {
		RespondAndExit(eventName)
		return
	}

	// 5.0 空开会话：Cursor 打开 Agent 后立刻关闭会打 sessionStart，无 Prompt 则不上报。
	if shouldSkipUnstartedLifecycle(eventName, currentPrompt, firstPrompt) {
		RespondAndExit(eventName)
		return
	}

	// 5.1 标签规则过滤 (如 require_tag: "#task")
	if strings.TrimSpace(effectiveCfg.RequireTag) != "" {
		hasTag := effectiveCfg.MatchesRequireTag(currentPrompt, firstPrompt, title)
		if hasTag {
			markSessionTracked(sessionID, firstPrompt)
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

	// 获取真实的常驻宿主 PID（若是 CLI/Bash 执行，优先使用 PPID）
	reportedPID := os.Getpid()
	if ppid := os.Getppid(); ppid > 1 {
		reportedPID = ppid
	}

	subType, subID, _ := extractSubagentMetadata(payload)
	parentID := extractParentID(payload)

	data := EventReport{
		ID:           sessionID,
		ParentID:     parentID,
		SubagentID:   subID,
		SubagentType: subType,
		Agent:        agentName,
		Repo:         fmt.Sprintf("%s:%s", repo, branch),
		Event:        mappedEvent,
		Timestamp:    time.Now().Unix(),
		Detail:       detail,
		TurnIndex:    turnCount,
		Title:        title,
		PID:          reportedPID,
		PGID:         os.Getppid(),
	}
	if len(currentPrompt) > 4000 {
		data.Prompt = currentPrompt[:4000]
	} else {
		data.Prompt = currentPrompt
	}
	if aiResponseText != "" {
		data.AIResponse = aiResponseText
	}

	// 10. 先补报失败队列，再发当前事件；接收服务端返回的控制决策
	_, ctrl := DeliverEventWithAction(cfg.ServerURL, cfg.APIKey, data)

	// 若服务端要求中断，在本地标记 sessionAborting，使得后续即便是 IgnoredTools 也被穿透拦截
	if ctrl.Action == "deny" || ctrl.Action == "abort" {
		markSessionAborting(sessionID)
	} else if mappedEvent == "agentCompletion" || mappedEvent == "failed" {
		unmarkSessionAborting(sessionID)
	}

	// 11. 根据控制决策输出响应协议（支持从 Web 看板反向中断/拒绝）
	RespondWithAction(eventName, agentName, ctrl)
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

func extractParentID(payload Payload) string {
	if payload.ParentID != "" {
		return payload.ParentID
	}
	if payload.ParentIDCamel != "" {
		return payload.ParentIDCamel
	}
	if payload.ParentSessionID != "" {
		return payload.ParentSessionID
	}
	if p := os.Getenv("PARENT_SESSION_ID"); p != "" {
		return p
	}
	return ""
}

func extractSubagentMetadata(payload Payload) (subType, subID, desc string) {
	subType = payload.SubagentType
	if subType == "" {
		subType = payload.SubagentTypeCamel
	}
	subID = payload.SubagentID
	if subID == "" {
		subID = payload.SubagentIDCamel
	}

	m := toolInputAsMap(payload)
	if m == nil {
		m = payload.ToolArgs
	}
	if m == nil {
		m = payload.Parameters
	}

	if m != nil {
		if subType == "" {
			for _, k := range []string{"subagent_type", "subagentType", "type", "role"} {
				if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
					subType = strings.TrimSpace(v)
					break
				}
			}
		}
		if subID == "" {
			for _, k := range []string{"agentId", "agent_id", "subagent_id", "subagentId", "id"} {
				if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
					subID = strings.TrimSpace(v)
					break
				}
			}
		}
		for _, k := range []string{"description", "prompt", "task", "message"} {
			if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
				desc = strings.TrimSpace(v)
				break
			}
		}
	}
	return subType, subID, desc
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
	if isAgentTool(toolName) || eventName == "subagentStart" || eventName == "subagentStop" {
		if eventName == "subagentStop" {
			return "子代理结束"
		}
		subType, _, desc := extractSubagentMetadata(payload)
		if subType != "" && desc != "" {
			return fmt.Sprintf("派发子智能体 [%s]: %s", subType, ShortTitle(desc))
		}
		if subType != "" {
			return fmt.Sprintf("派发子智能体 [%s]", subType)
		}
		if desc != "" {
			return fmt.Sprintf("派发子智能体: %s", ShortTitle(desc))
		}
		if s, ok := payload.Task.(string); ok && strings.TrimSpace(s) != "" {
			return fmt.Sprintf("子代理: %s", ShortTitle(s))
		}
		return "派发子智能体"
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

	// 合并会话本地持久化已记录的 Prompt 历史（针对无本地 transcript 文件的 Agent，如 ZCode / Codex CLI 等）
	if len(allPrompts) == 0 && sessionID != "" {
		allPrompts = readSessionPrompts(sessionID)
	}

	if directPrompt != "" {
		if !containsPrompt(allPrompts, directPrompt) {
			allPrompts = append(allPrompts, directPrompt)
		}
		if sessionID != "" {
			recordSessionPrompt(sessionID, directPrompt)
		}
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

	// 兜底补齐：如果 firstPrompt 丢失（例如后续轮次或单条工具事件没有历史 prompt），从已激活的追踪记录恢复首轮 prompt
	if (firstPrompt == "" || firstPrompt == currentPrompt) && sessionID != "" {
		if savedFirst := getSessionTrackedPrompt(sessionID); savedFirst != "" {
			firstPrompt = savedFirst
		}
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
// （附件、工作区快照等），避免 turn_index 虚高把仍在 running 的一轮顶掉。
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
	for _, envK := range []string{"CURSOR_PROJECT_DIR", "ZCODE_PROJECT_DIR", "CODEX_PROJECT_DIR", "CODEX_WORKSPACE_ROOT", "CLAUDE_PROJECT_DIR", "TRAE_PROJECT_DIR", "WINDSURF_PROJECT_DIR"} {
		if p := os.Getenv(envK); p != "" {
			roots = append(roots, p)
		}
	}
	roots = append(roots, payload.WorkspaceRoots...)

	home, err := os.UserHomeDir()
	if err == nil {
		// 全局通用 Codex 会话路径
		baseCodex := filepath.Join(home, ".codex", "sessions", sessionID)
		paths = append(paths, filepath.Join(baseCodex, "transcript.jsonl"))
		paths = append(paths, filepath.Join(home, ".codex", "sessions", sessionID+".jsonl"))
		paths = append(paths, filepath.Join(home, ".codex", "history.jsonl"))

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

			// Codex project transcripts
			paths = append(paths, filepath.Join(home, ".codex", "projects", slug, "transcripts", sessionID+".jsonl"))
			paths = append(paths, filepath.Join(root, ".codex", "sessions", sessionID+".jsonl"))
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
	for _, envK := range []string{"ZCODE_PROJECT_DIR", "CURSOR_PROJECT_DIR", "CODEX_PROJECT_DIR", "CODEX_WORKSPACE_ROOT", "CODEX_CWD", "CLAUDE_PROJECT_DIR", "TRAE_PROJECT_DIR", "WINDSURF_PROJECT_DIR"} {
		if v := os.Getenv(envK); v != "" {
			return v
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// GetGitInfo 获取 Git 仓库名与分支名（纯 Go 递归向上查找，支持 .git 目录及 submodule/worktree 的 gitdir 文件）
func GetGitInfo(payload Payload) (string, string) {
	startDir := ResolveWorkspaceRoot(payload)
	if startDir == "" {
		startDir, _ = os.Getwd()
	}

	repoRoot, gitDir := findGitRoot(startDir)
	if repoRoot == "" {
		repo := filepath.Base(startDir)
		if repo == "" || repo == "." || repo == "/" {
			repo = "workspace"
		}
		return repo, "main"
	}

	repo := filepath.Base(repoRoot)
	branch := "main"

	// 读取 gitDir/HEAD 解析分支
	headPath := filepath.Join(gitDir, "HEAD")
	if headData, err := os.ReadFile(headPath); err == nil {
		headStr := strings.TrimSpace(string(headData))
		if strings.HasPrefix(headStr, "ref: refs/heads/") {
			branch = strings.TrimPrefix(headStr, "ref: refs/heads/")
		} else if len(headStr) >= 7 {
			branch = headStr[:7] // Detached HEAD SHA
		}
	}

	return repo, branch
}

// findGitRoot 沿目录树递归向上查找 .git，返回 (工作区根目录, 实际 gitdir 目录)，绝不派生外部进程
func findGitRoot(dir string) (string, string) {
	curr := filepath.Clean(dir)
	for {
		gitEntry := filepath.Join(curr, ".git")
		fi, err := os.Stat(gitEntry)
		if err == nil {
			if fi.IsDir() {
				// 普通 Git 仓库根目录
				return curr, gitEntry
			}
			// Submodule 或 Worktree：.git 是一个包含 "gitdir: <path>" 的文件
			if data, rErr := os.ReadFile(gitEntry); rErr == nil {
				content := strings.TrimSpace(string(data))
				if strings.HasPrefix(content, "gitdir:") {
					relPath := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))
					if !filepath.IsAbs(relPath) {
						relPath = filepath.Join(curr, relPath)
					}
					return curr, filepath.Clean(relPath)
				}
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break // 到达根目录
		}
		curr = parent
	}
	return "", ""
}

// 快速本地网络熔断器（Fast Fail Circuit Breaker）
const circuitBreakerCooldown = 5 * time.Second

func circuitBreakerFile(serverURL string) string {
	sum := sha256.Sum256([]byte(serverURL))
	hashKey := hex.EncodeToString(sum[:8])
	return filepath.Join(os.TempDir(), fmt.Sprintf("agent-monitor-cb-%s.state", hashKey))
}

func isCircuitBreakerOpen(serverURL string) bool {
	p := circuitBreakerFile(serverURL)
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix()-ts < int64(circuitBreakerCooldown/time.Second)
}

func tripCircuitBreaker(serverURL string) {
	p := circuitBreakerFile(serverURL)
	_ = os.WriteFile(p, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}

func resetCircuitBreaker(serverURL string) {
	p := circuitBreakerFile(serverURL)
	_ = os.Remove(p)
}

// DeliverEvent 先补报本地 spool，再发送当前事件。Monitor 不可达时落盘，保证下次 Hook 能按序补报。
func DeliverEvent(serverURL string, report EventReport) {
	_, _ = DeliverEventWithAction(serverURL, "", report)
}

// DeliverEventWithKey 支持附带 API Key 进行补发和投递。
func DeliverEventWithKey(serverURL, apiKey string, report EventReport) {
	_, _ = DeliverEventWithAction(serverURL, apiKey, report)
}

// DeliverEventWithAction 先补报本地 spool，再发送当前事件并返回服务端的控制决策。
func DeliverEventWithAction(serverURL, apiKey string, report EventReport) (bool, ServerControlResponse) {
	defaultCtrl := ServerControlResponse{Action: "allow"}

	// 1. 快速熔断：若处于熔断冷却期，直接落盘并快速返回，耗时 < 0.1ms
	if isCircuitBreakerOpen(serverURL) {
		enqueueSpool(report)
		return false, defaultCtrl
	}

	// 2. 尝试清空本地历史积压
	flushOK := flushSpoolWithKey(serverURL, apiKey, spoolFlushLimit)
	if !flushOK {
		// flush 失败说明网络已断/服务端已挂，已自动触发熔断
		// 严禁发起第二次网络超时（杜绝 1.6s 级联卡顿），直接落盘后快速放行
		enqueueSpool(report)
		return false, defaultCtrl
	}

	// 3. 发送当前事件
	success, ctrl := SendEventWithAction(serverURL, apiKey, report)
	if !success {
		tripCircuitBreaker(serverURL)
		enqueueSpool(report)
		return false, ctrl
	}

	resetCircuitBreaker(serverURL)
	return success, ctrl
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

func spoolLockFile() string {
	return spoolFile() + ".lock"
}

// truncateSpoolKeepTail 丢弃旧数据，仅保留文件尾部最新的 targetBytes 字节（按行对齐）
func truncateSpoolKeepTail(path string, targetBytes int64) {
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) <= targetBytes {
		return
	}
	start := len(raw) - int(targetBytes)
	idx := bytes.IndexByte(raw[start:], '\n')
	if idx >= 0 {
		start += idx + 1
	}
	_ = os.WriteFile(path, raw[start:], 0644)
}

func enqueueSpool(report EventReport) {
	data, err := json.Marshal(report)
	if err != nil {
		return
	}

	_ = withSpoolLock(func() error {
		path := spoolFile()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		// 检查大小：若达到 maxSpoolFileBytes (2MB)，保留后半部分 (1MB)，丢弃过旧数据
		if info, err := os.Stat(path); err == nil && info.Size() >= maxSpoolFileBytes {
			truncateSpoolKeepTail(path, maxSpoolFileBytes/2)
		}

		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()

		_, _ = f.Write(append(data, '\n'))
		return nil
	})
}

func flushSpool(serverURL string, limit int) {
	_ = flushSpoolWithKey(serverURL, "", limit)
}

func flushSpoolWithKey(serverURL, apiKey string, limit int) bool {
	if limit <= 0 {
		limit = spoolFlushLimit
	}

	var pending [][]byte
	var path string

	// 在跨进程文件锁保护下原子读取并提取 pending 队列
	err := withSpoolLock(func() error {
		path = spoolFile()
		raw, rErr := os.ReadFile(path)
		if rErr != nil || len(raw) == 0 {
			return nil
		}

		for _, line := range bytes.Split(raw, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				pending = append(pending, line)
			}
		}
		_ = os.Remove(path) // 排空时彻底移除文件，避免留存 0 字节空文件导致检查异常
		return nil
	})

	if err != nil || len(pending) == 0 {
		return true
	}

	for i, line := range pending {
		if i >= limit {
			restoreSpoolLines(pending[i:])
			return true
		}
		var report EventReport
		if err := json.Unmarshal(line, &report); err != nil {
			continue // 脏数据跳过
		}
		if ok, _ := SendEventWithAction(serverURL, apiKey, report); !ok {
			tripCircuitBreaker(serverURL)
			restoreSpoolLines(pending[i:])
			return false
		}
	}
	return true
}

func restoreSpoolLines(lines [][]byte) {
	if len(lines) == 0 {
		return
	}
	_ = withSpoolLock(func() error {
		path := spoolFile()
		existing, _ := os.ReadFile(path)

		var buf bytes.Buffer
		for _, l := range lines {
			buf.Write(l)
			buf.WriteByte('\n')
		}
		if len(existing) > 0 {
			buf.Write(existing)
		}
		_ = os.WriteFile(path, buf.Bytes(), 0644)
		return nil
	})
}

// SendEvent 向 Monitor 发送单条事件，成功返回 true。失败由调用方 spool，绝不 panic。
func SendEvent(serverURL string, report EventReport) bool {
	success, _ := SendEventWithAction(serverURL, "", report)
	return success
}

// SendEventWithKey 向 Monitor 发送单条事件并附带 API Key 鉴权头。
func SendEventWithKey(serverURL, apiKey string, report EventReport) bool {
	success, _ := SendEventWithAction(serverURL, apiKey, report)
	return success
}

// SendEventWithAction 向 Monitor 发送单条事件并解析返回的决策指令。
func SendEventWithAction(serverURL, apiKey string, report EventReport) (bool, ServerControlResponse) {
	var controlResp ServerControlResponse
	controlResp.Action = "allow"

	data, err := json.Marshal(report)
	if err != nil {
		return false, controlResp
	}

	req, err := http.NewRequest("POST", serverURL, bytes.NewReader(data))
	if err != nil {
		return false, controlResp
	}
	req.Header.Set("Content-Type", "application/json")

	if apiKey == "" {
		apiKey = os.Getenv("AGENT_MONITOR_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("MONITOR_API_KEY")
		}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return false, controlResp
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = json.Unmarshal(respBody, &controlResp)
		if controlResp.Action == "" {
			controlResp.Action = "allow"
		}
		return true, controlResp
	}
	return false, controlResp
}

// DeleteSession 向 Monitor 发送 DELETE /api/tasks/{sessionID} 请求以移除该会话
func DeleteSession(serverURL, apiKey, sessionID string) bool {
	if sessionID == "" || serverURL == "" {
		return false
	}

	// 派生 DELETE 目标 URL：通常 serverURL 是形如 http://127.0.0.1:8000/api/event
	targetURL := strings.TrimSuffix(serverURL, "/api/event")
	targetURL = strings.TrimSuffix(targetURL, "/api/report")
	targetURL = strings.TrimSuffix(targetURL, "/")
	targetURL = fmt.Sprintf("%s/api/tasks/%s", targetURL, sessionID)

	req, err := http.NewRequest("DELETE", targetURL, nil)
	if err != nil {
		return false
	}

	if apiKey == "" {
		apiKey = os.Getenv("AGENT_MONITOR_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("MONITOR_API_KEY")
		}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
