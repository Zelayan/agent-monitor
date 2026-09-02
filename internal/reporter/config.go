package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// GlobalConfig 表示全局与项目级通用配置结构
type GlobalConfig struct {
	RequireTag  string   `json:"require_tag,omitempty"`  // 过滤标签（如 "#task" 或 "#task,#todo"；设为 "" 可强制全量）
	ServerURL   string   `json:"server_url,omitempty"`   // 监控服务 API 地址
	Disabled    bool     `json:"disabled,omitempty"`     // 是否禁用监控
	FilterRepos []string `json:"filter_repos,omitempty"` // 仅监控的仓库名白名单（可选）
}

// DefaultConfigPath 返回标准全局配置文件路径 ~/.agent-monitor/config.json
func DefaultConfigPath() string {
	if custom := os.Getenv("AGENT_MONITOR_CONFIG"); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".agent-monitor", "config.json")
}

// FindProjectConfigFile 在指定工作区根目录下查找项目级配置文件
// 优先级：.agent-monitor.json -> .agent-monitor/config.json
func FindProjectConfigFile(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(workspaceRoot, ".agent-monitor.json"),
		filepath.Join(workspaceRoot, ".agent-monitor", "config.json"),
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// LoadConfigForWorkspace 支持分层合并配置：
// 1. 全局配置 ~/.agent-monitor/config.json
// 2. 项目级配置 <workspace>/.agent-monitor.json（覆盖全局）
// 3. 环境变量（覆盖文件配置）
func LoadConfigForWorkspace(workspaceRoot string) GlobalConfig {
	var cfg GlobalConfig

	// 1. 读取全局配置
	globalPath := DefaultConfigPath()
	if globalPath != "" {
		if data, err := os.ReadFile(globalPath); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &cfg)
		} else {
			home, _ := os.UserHomeDir()
			if home != "" {
				altPath := filepath.Join(home, ".config", "agent-monitor", "config.json")
				if altData, altErr := os.ReadFile(altPath); altErr == nil && len(altData) > 0 {
					_ = json.Unmarshal(altData, &cfg)
				}
			}
		}
	}

	// 2. 查找并合并项目级配置文件（可覆盖全局 require_tag / server_url / disabled）
	projPath := FindProjectConfigFile(workspaceRoot)
	if projPath != "" {
		if data, err := os.ReadFile(projPath); err == nil && len(data) > 0 {
			var projCfg map[string]interface{}
			if err := json.Unmarshal(data, &projCfg); err == nil {
				if tagVal, ok := projCfg["require_tag"].(string); ok {
					cfg.RequireTag = tagVal
				}
				if urlVal, ok := projCfg["server_url"].(string); ok && urlVal != "" {
					cfg.ServerURL = urlVal
				}
				if disVal, ok := projCfg["disabled"].(bool); ok {
					cfg.Disabled = disVal
				}
			}
		}
	}

	// 3. 环境变量最高层级覆盖
	if envTag := os.Getenv("AGENT_MONITOR_REQUIRE_TAG"); envTag != "" {
		cfg.RequireTag = envTag
	}
	if envURL := os.Getenv("AGENT_MONITOR_URL"); envURL != "" {
		cfg.ServerURL = envURL
	} else if envServer := os.Getenv("MONITOR_SERVER_URL"); envServer != "" {
		cfg.ServerURL = envServer
	}
	if envDisabled := os.Getenv("AGENT_MONITOR_DISABLED"); envDisabled == "1" || strings.ToLower(envDisabled) == "true" {
		cfg.Disabled = true
	}

	return cfg
}

// LoadGlobalConfig 读取并解析全局配置文件；若文件不存在则返回默认空配置，绝不报错
func LoadGlobalConfig() GlobalConfig {
	path := DefaultConfigPath()
	if path == "" {
		return GlobalConfig{}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// 备用兼容：查找 ~/.config/agent-monitor/config.json
		home, _ := os.UserHomeDir()
		if home != "" {
			altPath := filepath.Join(home, ".config", "agent-monitor", "config.json")
			if altData, altErr := os.ReadFile(altPath); altErr == nil {
				data = altData
			}
		}
	}

	var cfg GlobalConfig
	if len(data) > 0 {
		_ = json.Unmarshal(data, &cfg)
	}

	// 环境变量层级覆盖（优先级高于文件）
	if envTag := os.Getenv("AGENT_MONITOR_REQUIRE_TAG"); envTag != "" {
		cfg.RequireTag = envTag
	}
	if envURL := os.Getenv("AGENT_MONITOR_URL"); envURL != "" {
		cfg.ServerURL = envURL
	} else if envServer := os.Getenv("MONITOR_SERVER_URL"); envServer != "" {
		cfg.ServerURL = envServer
	}
	if envDisabled := os.Getenv("AGENT_MONITOR_DISABLED"); envDisabled == "1" || strings.ToLower(envDisabled) == "true" {
		cfg.Disabled = true
	}

	return cfg
}

// MatchesRequireTag 检查文本是否命中任一配置的 require_tag（如 "#task"）
func (c *GlobalConfig) MatchesRequireTag(texts ...string) bool {
	tagStr := strings.TrimSpace(c.RequireTag)
	if tagStr == "" {
		return true // 未设置任何过滤标签，默认全量放行
	}

	tags := strings.Split(tagStr, ",")
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		for _, text := range texts {
			if strings.Contains(text, t) {
				return true
			}
		}
	}
	return false
}

// WriteDefaultConfigFile 将配置写入指定路径
func WriteDefaultConfigFile(cfg GlobalConfig, targetPath string) error {
	if targetPath == "" {
		targetPath = DefaultConfigPath()
	}
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// 不忽略空字段，确保如 require_tag: "" 能被显式写出以覆盖全局
	m := map[string]interface{}{
		"require_tag": cfg.RequireTag,
		"server_url":  cfg.ServerURL,
		"disabled":    cfg.Disabled,
	}
	if len(cfg.FilterRepos) > 0 {
		m["filter_repos"] = cfg.FilterRepos
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0644)
}
