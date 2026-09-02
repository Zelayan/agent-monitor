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

	// 3. 多标签（逗号分隔）
	multiCfg := GlobalConfig{RequireTag: "#task,#todo,[monitor]"}
	if !multiCfg.MatchesRequireTag("这是一个 [monitor] 任务") {
		t.Fatalf("expected [monitor] to match")
	}
	if !multiCfg.MatchesRequireTag("#todo 修复前端样式") {
		t.Fatalf("expected #todo to match")
	}
	if multiCfg.MatchesRequireTag("没有任何前缀的请求") {
		t.Fatalf("expected no match")
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
	defer os.Unsetenv("AGENT_MONITOR_REQUIRE_TAG")

	overridden := LoadGlobalConfig()
	if overridden.RequireTag != "#custom" {
		t.Fatalf("expected RequireTag '#custom' from env, got %q", overridden.RequireTag)
	}
}

func TestSessionTracking(t *testing.T) {
	sessionID := "test-session-track-123"
	unmarkSessionTracked(sessionID)

	if isSessionTracked(sessionID) {
		t.Fatalf("expected session not tracked initially")
	}

	markSessionTracked(sessionID)
	if !isSessionTracked(sessionID) {
		t.Fatalf("expected session to be tracked after markSessionTracked")
	}

	unmarkSessionTracked(sessionID)
	if isSessionTracked(sessionID) {
		t.Fatalf("expected session not tracked after unmarkSessionTracked")
	}
}
