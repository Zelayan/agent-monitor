package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultRequireTag 定义开箱即用默认过滤标签
const DefaultRequireTag = "#task"

// DefaultDeleteTag 定义开箱即用默认会话删除/丢弃标签
const DefaultDeleteTag = "#drop,#untrack"

// GlobalConfig 表示全局与项目级通用配置结构
type GlobalConfig struct {
	RequireTag  string   `json:"require_tag,omitempty"`  // 过滤标签（默认 "#task"；设为 "" 可强制全量放行）
	DeleteTag   string   `json:"delete_tag,omitempty"`   // 会话删除/取消追踪标签（默认 "#drop,#untrack"）
	ServerURL   string   `json:"server_url,omitempty"`   // 监控服务 API 地址
	Disabled    bool     `json:"disabled,omitempty"`     // 是否禁用监控
	FilterRepos []string `json:"filter_repos,omitempty"` // 仅监控的仓库名白名单（可选）
	APIKey      string   `json:"api_key,omitempty"`      // 监控服务鉴权 API Key（可选）
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
// 1. 默认值：require_tag 默认为 "#task"
// 2. 全局配置 ~/.agent-monitor/config.json
// 3. 项目级配置 <workspace>/.agent-monitor.json（覆盖全局）
// 4. 环境变量（覆盖文件配置）
func LoadConfigForWorkspace(workspaceRoot string) GlobalConfig {
	cfg := GlobalConfig{
		RequireTag: DefaultRequireTag,
		DeleteTag:  DefaultDeleteTag,
	}

	// 1. 读取全局配置
	globalPath := DefaultConfigPath()
	var globalData []byte
	if globalPath != "" {
		if data, err := os.ReadFile(globalPath); err == nil && len(data) > 0 {
			globalData = data
		} else {
			home, _ := os.UserHomeDir()
			if home != "" {
				altPath := filepath.Join(home, ".config", "agent-monitor", "config.json")
				if altData, altErr := os.ReadFile(altPath); altErr == nil && len(altData) > 0 {
					globalData = altData
				}
			}
		}
	}
	if len(globalData) > 0 {
		var globalMap map[string]interface{}
		if err := json.Unmarshal(globalData, &globalMap); err == nil {
			if tagVal, ok := globalMap["require_tag"].(string); ok {
				cfg.RequireTag = tagVal
			} else if tagList, ok := globalMap["require_tag"].([]interface{}); ok {
				var tags []string
				for _, item := range tagList {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						tags = append(tags, strings.TrimSpace(s))
					}
				}
				cfg.RequireTag = strings.Join(tags, ",")
			}
			if delVal, ok := globalMap["delete_tag"].(string); ok {
				cfg.DeleteTag = delVal
			} else if delList, ok := globalMap["delete_tag"].([]interface{}); ok {
				var tags []string
				for _, item := range delList {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						tags = append(tags, strings.TrimSpace(s))
					}
				}
				cfg.DeleteTag = strings.Join(tags, ",")
			}
			if urlVal, ok := globalMap["server_url"].(string); ok && urlVal != "" {
				cfg.ServerURL = urlVal
			}
			if disVal, ok := globalMap["disabled"].(bool); ok {
				cfg.Disabled = disVal
			}
			if keyVal, ok := globalMap["api_key"].(string); ok && keyVal != "" {
				cfg.APIKey = keyVal
			}
		}
	}

	// 2. 查找并合并项目级配置文件（可覆盖全局 require_tag / server_url / disabled / api_key）
	projPath := FindProjectConfigFile(workspaceRoot)
	if projPath != "" {
		if data, err := os.ReadFile(projPath); err == nil && len(data) > 0 {
			var projCfg map[string]interface{}
			if err := json.Unmarshal(data, &projCfg); err == nil {
				if tagVal, ok := projCfg["require_tag"].(string); ok {
					cfg.RequireTag = tagVal
				} else if tagList, ok := projCfg["require_tag"].([]interface{}); ok {
					var tags []string
					for _, item := range tagList {
						if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
							tags = append(tags, strings.TrimSpace(s))
						}
					}
					cfg.RequireTag = strings.Join(tags, ",")
				}
				if delVal, ok := projCfg["delete_tag"].(string); ok {
					cfg.DeleteTag = delVal
				} else if delList, ok := projCfg["delete_tag"].([]interface{}); ok {
					var tags []string
					for _, item := range delList {
						if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
							tags = append(tags, strings.TrimSpace(s))
						}
					}
					cfg.DeleteTag = strings.Join(tags, ",")
				}
				if urlVal, ok := projCfg["server_url"].(string); ok && urlVal != "" {
					cfg.ServerURL = urlVal
				}
				if disVal, ok := projCfg["disabled"].(bool); ok {
					cfg.Disabled = disVal
				}
				if keyVal, ok := projCfg["api_key"].(string); ok && keyVal != "" {
					cfg.APIKey = keyVal
				}
			}
		}
	}

	// 3. 环境变量最高层级覆盖
	if envTag, ok := os.LookupEnv("AGENT_MONITOR_REQUIRE_TAG"); ok {
		cfg.RequireTag = envTag
	}
	if envDelTag, ok := os.LookupEnv("AGENT_MONITOR_DELETE_TAG"); ok {
		cfg.DeleteTag = envDelTag
	}
	if envURL := os.Getenv("AGENT_MONITOR_URL"); envURL != "" {
		cfg.ServerURL = envURL
	} else if envServer := os.Getenv("MONITOR_SERVER_URL"); envServer != "" {
		cfg.ServerURL = envServer
	}
	if envKey := os.Getenv("AGENT_MONITOR_API_KEY"); envKey != "" {
		cfg.APIKey = envKey
	} else if envKeyAlt := os.Getenv("MONITOR_API_KEY"); envKeyAlt != "" {
		cfg.APIKey = envKeyAlt
	}
	if envDisabled := os.Getenv("AGENT_MONITOR_DISABLED"); envDisabled == "1" || strings.ToLower(envDisabled) == "true" {
		cfg.Disabled = true
	}

	return cfg
}

// LoadGlobalConfig 读取并解析全局配置文件；若文件不存在则返回默认配置（require_tag 默认为 "#task"），绝不报错
func LoadGlobalConfig() GlobalConfig {
	cfg := GlobalConfig{
		RequireTag: DefaultRequireTag,
		DeleteTag:  DefaultDeleteTag,
	}

	path := DefaultConfigPath()
	if path == "" {
		return cfg
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

	if len(data) > 0 {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(data, &rawMap); err == nil {
			if tagVal, ok := rawMap["require_tag"].(string); ok {
				cfg.RequireTag = tagVal
			} else if tagList, ok := rawMap["require_tag"].([]interface{}); ok {
				var tags []string
				for _, item := range tagList {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						tags = append(tags, strings.TrimSpace(s))
					}
				}
				cfg.RequireTag = strings.Join(tags, ",")
			}
			if delVal, ok := rawMap["delete_tag"].(string); ok {
				cfg.DeleteTag = delVal
			} else if delList, ok := rawMap["delete_tag"].([]interface{}); ok {
				var tags []string
				for _, item := range delList {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						tags = append(tags, strings.TrimSpace(s))
					}
				}
				cfg.DeleteTag = strings.Join(tags, ",")
			}
			if urlVal, ok := rawMap["server_url"].(string); ok {
				cfg.ServerURL = urlVal
			}
			if disVal, ok := rawMap["disabled"].(bool); ok {
				cfg.Disabled = disVal
			}
			if keyVal, ok := rawMap["api_key"].(string); ok {
				cfg.APIKey = keyVal
			}
		}
	}

	// 环境变量层级覆盖（优先级高于文件）
	if envTag, ok := os.LookupEnv("AGENT_MONITOR_REQUIRE_TAG"); ok {
		cfg.RequireTag = envTag
	}
	if envDelTag, ok := os.LookupEnv("AGENT_MONITOR_DELETE_TAG"); ok {
		cfg.DeleteTag = envDelTag
	}
	if envURL := os.Getenv("AGENT_MONITOR_URL"); envURL != "" {
		cfg.ServerURL = envURL
	} else if envServer := os.Getenv("MONITOR_SERVER_URL"); envServer != "" {
		cfg.ServerURL = envServer
	}
	if envKey := os.Getenv("AGENT_MONITOR_API_KEY"); envKey != "" {
		cfg.APIKey = envKey
	} else if envKeyAlt := os.Getenv("MONITOR_API_KEY"); envKeyAlt != "" {
		cfg.APIKey = envKeyAlt
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

	// MatchesDeleteTag 检查文本是否包含指定删除/取消追踪标签（如 "#drop", "#untrack"）
	// 为防止代码子串误触，要求按独立 Token / 词边界匹配
	func (c *GlobalConfig) MatchesDeleteTag(texts ...string) bool {
		tagStr := strings.TrimSpace(c.DeleteTag)
		if tagStr == "" || tagStr == "none" {
			return false
		}

		tags := strings.Split(tagStr, ",")
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			// 匹配以空格、标点或行首行尾为边界的标签，支持忽略大小写
			pattern := `(?i)(?:^|[\s,;，。！？!?]|^)` + regexp.QuoteMeta(t) + `(?:$|[\s,;，。！？!?]|$)`
			re, err := regexp.Compile(pattern)
			if err != nil {
				// 正则编译兜底
				for _, text := range texts {
					if strings.Contains(text, t) {
						return true
					}
				}
				continue
			}
			for _, text := range texts {
				if re.MatchString(text) {
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
			"delete_tag":  cfg.DeleteTag,
			"server_url":  cfg.ServerURL,
			"disabled":    cfg.Disabled,
		}
		if cfg.APIKey != "" {
			m["api_key"] = cfg.APIKey
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
