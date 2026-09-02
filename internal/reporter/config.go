package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// GlobalConfig 表示 ~/.agent-monitor/config.json 全局统一配置结构
type GlobalConfig struct {
	RequireTag  string   `json:"require_tag,omitempty"`  // 过滤标签（如 "#task" 或 "#task,#todo"）
	ServerURL   string   `json:"server_url,omitempty"`   // 监控服务 API 地址
	Disabled    bool     `json:"disabled,omitempty"`     // 全局是否禁用监控
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

// LoadGlobalConfig 读取并解析配置文件；若文件不存在则返回默认空配置，绝不报错
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

// WriteDefaultConfigFile 将配置写入指定路径（通常为 ~/.agent-monitor/config.json）
func WriteDefaultConfigFile(cfg GlobalConfig, targetPath string) error {
	if targetPath == "" {
		targetPath = DefaultConfigPath()
	}
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0644)
}
