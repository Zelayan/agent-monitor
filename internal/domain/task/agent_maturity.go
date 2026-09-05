package task

import "strings"

// MaturityTier 定义 Agent 适配成熟度等级（3-tier 分级体系）。
type MaturityTier string

const (
	// MaturityOfficial 官方深度适配：生命周期、工具失败保护、多轮 Run、Transcript 解析、Fail-Safe 与自动化测试全覆盖。
	MaturityOfficial MaturityTier = "Official"
	// MaturityBeta 试验公测：核心生命周期可用，但部分工具或 Transcript 解析能力有限。
	MaturityBeta MaturityTier = "Beta"
	// MaturityExperimental 社区实验：仅提供基础事件上报或通用 HTTP 适配，不承诺完整状态机语义。
	MaturityExperimental MaturityTier = "Experimental"
)

// AgentMaturitySpec 记录特定 Agent 的成熟度画像与技术规范。
type AgentMaturitySpec struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	DisplayName    string       `json:"displayName"`
	Tier           MaturityTier `json:"tier"`
	Description    string       `json:"description"`
	HasLifecycle   bool         `json:"hasLifecycle"`
	HasToolFailure bool         `json:"hasToolFailure"`
	HasMultiTurn   bool         `json:"hasMultiTurn"`
	HasTranscript  bool         `json:"hasTranscript"`
		HasFailSafe    bool         `json:"hasFailSafe"`
		HookTypes      []string     `json:"hookTypes"`
	}

	// Clone 返回 AgentMaturitySpec 的深拷贝副本，杜绝底层切片数据竞争与状态污染
	func (s AgentMaturitySpec) Clone() AgentMaturitySpec {
		cloned := s
		if s.HookTypes != nil {
			cloned.HookTypes = make([]string, len(s.HookTypes))
			copy(cloned.HookTypes, s.HookTypes)
		}
		return cloned
	}

	// defaultCatalog 是 Agent Monitor 权威认证的 Agent 成熟度目录。
var defaultCatalog = []AgentMaturitySpec{
	// --- Official Agents ---
	{
		ID:             "cursor",
		Name:           "Cursor Agent",
		DisplayName:    "Cursor Agent",
		Tier:           MaturityOfficial,
		Description:    "Cursor IDE 原生智能体，全生命周期拦截、Subagent 级联、MCP/Shell 深度追踪与 Transcript 解析",
		HasLifecycle:   true,
		HasToolFailure: true,
		HasMultiTurn:   true,
		HasTranscript:  true,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "beforeSubmitPrompt", "preToolUse", "postToolUseFailure", "beforeShellExecution", "beforeMCPExecution", "subagentStart", "afterAgentResponse", "stop", "sessionEnd"},
	},
	{
		ID:             "zcode",
		Name:           "ZCode",
		DisplayName:    "ZCode CLI",
		Tier:           MaturityOfficial,
		Description:    "ZCode 终端智能体，微秒级 Hook 触发、子代理角色解析与反向控制拦截",
		HasLifecycle:   true,
		HasToolFailure: true,
		HasMultiTurn:   true,
		HasTranscript:  true,
		HasFailSafe:    true,
		HookTypes:      []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUseFailure", "Stop", "agentCompletion"},
	},
	{
		ID:             "codex-cli",
		Name:           "Codex CLI",
		DisplayName:    "Codex CLI",
		Tier:           MaturityOfficial,
		Description:    "Codex 命令行终端智能体，支持命令流拦截、多轮 Prompt 会话持久化与状态机收口",
		HasLifecycle:   true,
		HasToolFailure: true,
		HasMultiTurn:   true,
		HasTranscript:  true,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "beforeSubmitPrompt", "preToolUse", "postToolUseFailure", "beforeShellExecution", "stop", "sessionEnd"},
	},
	{
		ID:             "codex-desktop",
		Name:           "Codex Desktop",
		DisplayName:    "Codex Desktop",
		Tier:           MaturityOfficial,
		Description:    "Codex 桌面客户端，支持独立应用生命周期追踪、多工作区与 Transcript 会话提取",
		HasLifecycle:   true,
		HasToolFailure: true,
		HasMultiTurn:   true,
		HasTranscript:  true,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "beforeSubmitPrompt", "preToolUse", "postToolUseFailure", "afterAgentResponse", "stop", "sessionEnd"},
	},
	{
		ID:             "claude-code",
		Name:           "Claude Code",
		DisplayName:    "Claude Code (CLI)",
		Tier:           MaturityOfficial,
		Description:    "Anthropic Claude Code 官方终端智能体，完整 JSONL Transcript 解析、工具失败保护与非阻塞放行",
		HasLifecycle:   true,
		HasToolFailure: true,
		HasMultiTurn:   true,
		HasTranscript:  true,
		HasFailSafe:    true,
		HookTypes:      []string{"SessionStart", "sessionStart", "UserPromptSubmit", "userPrompt", "PreToolUse", "preToolUse", "toolUse", "PostToolUseFailure", "postToolUseFailure", "toolResult", "Stop", "stop", "agentCompletion"},
	},
	{
		ID:             "aider",
		Name:           "Aider",
		DisplayName:    "Aider (CLI / Git)",
		Tier:           MaturityOfficial,
		Description:    "Aider 终端配对编程助手，支持 Chat History / Input History 多轮解析与 Git Commit 轨迹集成",
		HasLifecycle:   true,
		HasToolFailure: true,
		HasMultiTurn:   true,
		HasTranscript:  true,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "userPrompt", "toolUse", "toolResult", "agentCompletion"},
	},

	// --- Beta Agents ---
	{
		ID:             "continue",
		Name:           "Continue",
		DisplayName:    "Continue.dev",
		Tier:           MaturityBeta,
		Description:    "VS Code / JetBrains 开源扩展，支持自定义 Analytics HTTP 事件上报与基础轮次展示",
		HasLifecycle:   true,
		HasToolFailure: false,
		HasMultiTurn:   true,
		HasTranscript:  false,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "agentCompletion"},
	},
	{
		ID:             "windsurf",
		Name:           "Windsurf",
		DisplayName:    "Windsurf (Cascade)",
		Tier:           MaturityBeta,
		Description:    "Codeium Cascade IDE，支持工作区环境感知与基础事件追踪",
		HasLifecycle:   true,
		HasToolFailure: false,
		HasMultiTurn:   true,
		HasTranscript:  false,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "agentCompletion"},
	},
	{
		ID:             "trae",
		Name:           "Trae",
		DisplayName:    "Trae IDE",
		Tier:           MaturityBeta,
		Description:    "ByteDance Trae 原生 AI IDE，支持会话级事件监听与状态呈现",
		HasLifecycle:   true,
		HasToolFailure: false,
		HasMultiTurn:   true,
		HasTranscript:  false,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "agentCompletion"},
	},

	// --- Experimental Agents ---
	{
		ID:             "generic",
		Name:           "AI Agent",
		DisplayName:    "Generic AI Agent",
		Tier:           MaturityExperimental,
		Description:    "通用自定义或第三方 AI 智能体，通过标准 HTTP API 上报，不承诺完整状态机一致性保证",
		HasLifecycle:   false,
		HasToolFailure: false,
		HasMultiTurn:   false,
		HasTranscript:  false,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "agentCompletion"},
	},
}

// GetMaturityCatalog 返回系统支持的全部 Agent 成熟度规格画像列表（深拷贝）。
func GetMaturityCatalog() []AgentMaturitySpec {
	copied := make([]AgentMaturitySpec, len(defaultCatalog))
	for i, spec := range defaultCatalog {
		copied[i] = spec.Clone()
	}
	return copied
}

// NormalizeAgentName 将常见别名或小写格式归一化为标准的 Agent 规范名称。
func NormalizeAgentName(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(lower, "cursor"):
		return "Cursor Agent"
	case strings.Contains(lower, "zcode"):
		return "ZCode"
	case strings.Contains(lower, "codex desktop") || strings.Contains(lower, "codex-desktop") || strings.Contains(lower, "codex_desktop") || strings.Contains(lower, "codex.app"):
		return "Codex Desktop"
	case strings.Contains(lower, "codex"):
		return "Codex CLI"
	case strings.Contains(lower, "claude"):
		return "Claude Code"
	case strings.Contains(lower, "aider"):
		return "Aider"
	case strings.Contains(lower, "continue"):
		return "Continue"
	case strings.Contains(lower, "windsurf") || strings.Contains(lower, "codeium"):
		return "Windsurf"
	case strings.Contains(lower, "trae"):
		return "Trae"
	default:
		if strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
		return "AI Agent"
	}
}

// ResolveAgentMaturity 根据 Agent 名称（支持模糊别名与大小写）解析其所属的成熟度等级。
func ResolveAgentMaturity(agentName string) MaturityTier {
	norm := NormalizeAgentName(agentName)
	for _, spec := range defaultCatalog {
		if strings.EqualFold(spec.Name, norm) || strings.EqualFold(spec.DisplayName, norm) || strings.EqualFold(spec.ID, norm) {
			return spec.Tier
		}
	}
	return MaturityExperimental
}

// GetAgentSpec 获取指定 Agent 的完整成熟度规格配置（深拷贝副本）。
func GetAgentSpec(agentName string) AgentMaturitySpec {
	norm := NormalizeAgentName(agentName)
	for _, spec := range defaultCatalog {
		if strings.EqualFold(spec.Name, norm) || strings.EqualFold(spec.DisplayName, norm) || strings.EqualFold(spec.ID, norm) {
			return spec.Clone()
		}
	}
	// 兜底返回 Experimental 规格
	return AgentMaturitySpec{
		ID:             "generic",
		Name:           norm,
		DisplayName:    norm,
		Tier:           MaturityExperimental,
		Description:    "第三方或自定义智能体",
		HasLifecycle:   false,
		HasToolFailure: false,
		HasMultiTurn:   false,
		HasTranscript:  false,
		HasFailSafe:    true,
		HookTypes:      []string{"sessionStart", "agentCompletion"},
	}
}

// ListAgentsByTier 获取指定成熟度等级的所有 Agent 规格列表（深拷贝副本）。
func ListAgentsByTier(tier MaturityTier) []AgentMaturitySpec {
	var results []AgentMaturitySpec
	for _, spec := range defaultCatalog {
		if spec.Tier == tier {
			results = append(results, spec.Clone())
		}
	}
	return results
}
