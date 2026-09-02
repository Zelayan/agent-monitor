package reporter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalConfig_MatchesRequireTag(t *testing.T) {
	// 1. 未配置任何 Tag：默认全量放行
	emptyCfg := GlobalConfig{}
	if !emptyCfg.MatchesRequireTag("hello world", "any prompt") {
		t.Fatalf("expected empty tag to match everything")
	}

	// 2. 单个 Tag 匹配
	taskCfg := GlobalConfig{RequireTag: "#task"}
	if !taskCfg.MatchesRequireTag("#task 重构登录模块", "其他文本") {
		t.Fatalf("expected #task to match prompt with #task")
	}
	if taskCfg.MatchesRequireTag("普通闲聊问题", "请帮我看看这个代码") {
		t.Fatalf("expected plain prompt without #task to be rejected")
	}

	// 3. 多标签（逗号分隔字符串）匹配
	multiCfg := GlobalConfig{RequireTag: "#task, #todo, [monitor]"}
	if !multiCfg.MatchesRequireTag("这是一个 [monitor] 任务") {
		t.Fatalf("expected [monitor] to match")
	}
	if !multiCfg.MatchesRequireTag("#todo 修复前端样式") {
		t.Fatalf("expected #todo to match")
	}
	if multiCfg.MatchesRequireTag("没有任何前缀的请求") {
		t.Fatalf("expected no match")
	}

	// 4. JSON 数组形式的多标签测试
	testDir, _ := os.MkdirTemp("", "agent-monitor-multi-tag-*")
	defer os.RemoveAll(testDir)

	jsonArrData := []byte(`{"require_tag": ["#task", "#todo", "#bug"]}`)
	tmpArrFile := filepath.Join(testDir, "array-config.json")
	_ = os.WriteFile(tmpArrFile, jsonArrData, 0644)
	os.Setenv("AGENT_MONITOR_CONFIG", tmpArrFile)
	arrCfg := LoadGlobalConfig()
	os.Unsetenv("AGENT_MONITOR_CONFIG")
	if !arrCfg.MatchesRequireTag("#bug 修复报错") {
		t.Fatalf("expected #bug to match array config")
	}
	if !arrCfg.MatchesRequireTag("#task 正常工作") {
		t.Fatalf("expected #task to match array config")
	}
	if arrCfg.MatchesRequireTag("无标签普通闲聊") {
		t.Fatalf("expected plain prompt to fail array config")
	}
}

func TestGlobalConfig_LoadAndWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-monitor-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.json")
	os.Setenv("AGENT_MONITOR_CONFIG", cfgPath)
	defer os.Unsetenv("AGENT_MONITOR_CONFIG")

	// 1. 写入配置文件
	origCfg := GlobalConfig{
		RequireTag: "#task",
		ServerURL:  "http://192.168.1.100:8000/api/event",
		Disabled:   false,
	}
	if err := WriteDefaultConfigFile(origCfg, cfgPath); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// 2. 加载验证
	loaded := LoadGlobalConfig()
	if loaded.RequireTag != "#task" {
		t.Fatalf("expected RequireTag '#task', got %q", loaded.RequireTag)
	}
	if loaded.ServerURL != "http://192.168.1.100:8000/api/event" {
		t.Fatalf("expected ServerURL 'http://192.168.1.100:8000/api/event', got %q", loaded.ServerURL)
	}

	// 3. 环境变量覆盖验证
	os.Setenv("AGENT_MONITOR_REQUIRE_TAG", "#custom")
	overridden := LoadGlobalConfig()
	os.Unsetenv("AGENT_MONITOR_REQUIRE_TAG")
	if overridden.RequireTag != "#custom" {
		t.Fatalf("expected RequireTag '#custom' from env, got %q", overridden.RequireTag)
	}
}

func TestLoadConfigForWorkspace_ProjectOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-monitor-workspace-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	globalPath := filepath.Join(tmpDir, "global-config.json")
	os.Setenv("AGENT_MONITOR_CONFIG", globalPath)
	defer os.Unsetenv("AGENT_MONITOR_CONFIG")

	// 1. 写入全局配置：要求 #task 标签
	globalCfg := GlobalConfig{
		RequireTag: "#task",
		ServerURL:  "http://127.0.0.1:8000/api/event",
		Disabled:   false,
	}
	if err := WriteDefaultConfigFile(globalCfg, globalPath); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	// 2. 在工作区没有项目配置时，读取工作区继承全局
	workDirA := filepath.Join(tmpDir, "project-a")
	_ = os.MkdirAll(workDirA, 0755)
	cfgA := LoadConfigForWorkspace(workDirA)
	if cfgA.RequireTag != "#task" {
		t.Fatalf("expected project A to inherit global tag '#task', got %q", cfgA.RequireTag)
	}

	// 3. 在工作区 B 创建项目级 .agent-monitor.json：重写为无需标签全量监控
	workDirB := filepath.Join(tmpDir, "project-b")
	_ = os.MkdirAll(workDirB, 0755)
	projCfgB := GlobalConfig{
		RequireTag: "", // 覆盖为空，代表全量监控
		ServerURL:  "http://192.168.1.50:8000/api/event",
	}
	if err := WriteDefaultConfigFile(projCfgB, filepath.Join(workDirB, ".agent-monitor.json")); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	cfgB := LoadConfigForWorkspace(workDirB)
	if cfgB.RequireTag != "" {
		t.Fatalf("expected project B require_tag to be overridden to empty, got %q", cfgB.RequireTag)
	}
		if cfgB.ServerURL != "http://192.168.1.50:8000/api/event" {
			t.Fatalf("expected project B server_url to be overridden, got %q", cfgB.ServerURL)
		}
	}

	func TestGlobalConfig_APIKeySupport(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "agent-monitor-apikey-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfgPath := filepath.Join(tmpDir, "config.json")
		os.Setenv("AGENT_MONITOR_CONFIG", cfgPath)
		defer os.Unsetenv("AGENT_MONITOR_CONFIG")

		// 1. 写入包含 API Key 的配置文件
		cfg := GlobalConfig{
			APIKey:    "test-secret-key-123",
			ServerURL: "http://127.0.0.1:8000/api/event",
		}
		if err := WriteDefaultConfigFile(cfg, cfgPath); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		// 2. 加载验证
		loaded := LoadGlobalConfig()
		if loaded.APIKey != "test-secret-key-123" {
			t.Fatalf("expected APIKey 'test-secret-key-123', got %q", loaded.APIKey)
		}

		// 3. 项目级覆盖测试
		projDir := filepath.Join(tmpDir, "my-project")
		_ = os.MkdirAll(projDir, 0755)
		projCfg := GlobalConfig{
			APIKey: "project-override-key",
		}
		if err := WriteDefaultConfigFile(projCfg, filepath.Join(projDir, ".agent-monitor.json")); err != nil {
			t.Fatalf("failed to write project config: %v", err)
		}
		loadedWorkspace := LoadConfigForWorkspace(projDir)
		if loadedWorkspace.APIKey != "project-override-key" {
			t.Fatalf("expected project APIKey 'project-override-key', got %q", loadedWorkspace.APIKey)
		}

		// 4. 环境变量覆盖最高优先级
		os.Setenv("AGENT_MONITOR_API_KEY", "env-top-secret")
		defer os.Unsetenv("AGENT_MONITOR_API_KEY")

		loadedEnv := LoadConfigForWorkspace(projDir)
		if loadedEnv.APIKey != "env-top-secret" {
			t.Fatalf("expected env APIKey 'env-top-secret', got %q", loadedEnv.APIKey)
		}
	}

	func TestGlobalConfig_DefaultRequireTag(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "agent-monitor-default-tag-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		nonexistent := filepath.Join(tmpDir, "nonexistent.json")
		os.Setenv("AGENT_MONITOR_CONFIG", nonexistent)
		defer os.Unsetenv("AGENT_MONITOR_CONFIG")
		os.Unsetenv("AGENT_MONITOR_REQUIRE_TAG")

		// 1. 无配置文件无环境变量时，默认过滤 #task
		cfg := LoadConfigForWorkspace(tmpDir)
		if cfg.RequireTag != DefaultRequireTag {
			t.Fatalf("expected default RequireTag '#task', got %q", cfg.RequireTag)
		}

		// 2. 环境变量显式配置为空字符串时，应该覆盖为全量放行
		os.Setenv("AGENT_MONITOR_REQUIRE_TAG", "")
		cfgEnv := LoadConfigForWorkspace(tmpDir)
		if cfgEnv.RequireTag != "" {
			t.Fatalf("expected empty RequireTag when AGENT_MONITOR_REQUIRE_TAG='', got %q", cfgEnv.RequireTag)
		}
	}
