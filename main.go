// AGENT MONITOR：面向 AI Coding Agent 的多会话实时监视器。
// 基于 DDD (领域驱动设计) 分层架构构建：
// - Domain: internal/domain/task (Task 聚合根、Turn 实体、Timeline 值对象、领域仓储接口)
// - Application: internal/application/monitor (MonitorService 用例编排、SSE Hub 广播)
// - Infrastructure: internal/infrastructure/persistence (JSON 仓储实现), internal/infrastructure/transport/http (HTTP 控制器)
package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Zelayan/agent-monitor/internal/application/monitor"
	"github.com/Zelayan/agent-monitor/internal/infrastructure/persistence"
	transport "github.com/Zelayan/agent-monitor/internal/infrastructure/transport/http"
)

//go:embed static/*
var staticFS embed.FS

//go:embed static/index.html
var indexHTML []byte // 将 Monitor 页面嵌入二进制

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data/sessions"
	}
	apiKey := os.Getenv("AGENT_MONITOR_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("MONITOR_API_KEY")
	}
	masterKey := os.Getenv("AGENT_MONITOR_MASTER_KEY")
	if masterKey == "" {
		masterKey = os.Getenv("MONITOR_MASTER_KEY")
	}
	apiKeys := os.Getenv("AGENT_MONITOR_API_KEYS")
	if apiKeys == "" {
		apiKeys = os.Getenv("MONITOR_API_KEYS")
	}

	flagPort := flag.String("port", "", "Server port (defaults to $PORT or 8000)")
	flagDataDir := flag.String("data-dir", "", "Session data storage directory (defaults to $DATA_DIR or data/sessions)")
	flagAPIKey := flag.String("api-key", "", "Default API Key for request authentication (defaults to $AGENT_MONITOR_API_KEY)")
	flagMasterKey := flag.String("master-key", "", "Master Key for global multi-project viewing and administration")
	flagAPIKeys := flag.String("api-keys", "", "Multi-project API keys mapping (format: projA=key_1,projB=key_2)")
	flag.Parse()

	if *flagPort != "" {
		port = *flagPort
	}
	if *flagDataDir != "" {
		dataDir = *flagDataDir
	}
	if *flagAPIKey != "" {
		apiKey = *flagAPIKey
	}
	if *flagMasterKey != "" {
		masterKey = *flagMasterKey
	}
	if *flagAPIKeys != "" {
		apiKeys = *flagAPIKeys
	}

	// 1. 基础设施层：初始化持久化仓储
	repo, err := persistence.NewJSONRepository(dataDir)
	if err != nil {
		log.Printf("[Repository] Warning: failed to initialize JSON repository in %s: %v", dataDir, err)
	} else {
		defer repo.Close()
	}

	// 2. 应用层：初始化 SSE 广播中心并启动事件循环
	hub := monitor.NewHub()
	go hub.Run()

	// 3. 应用层：初始化 Monitor 应用服务并恢复已有会话
	svc := monitor.NewMonitorService(repo, hub)
	if sum := monitor.NewTitleSummarizerFromEnv(); sum != nil {
		svc.SetTitleSummarizer(sum)
		log.Printf("[Application] LLM session titles enabled")
	}

	// 4. 用户接口层 / HTTP 适配器：注册路由
	handler := transport.NewHandler(svc, hub, indexHTML).
		WithStaticFS(staticFS).
		WithAPIKey(apiKey).
		WithMasterKey(masterKey).
		WithProjectKeys(apiKeys)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	addr := ":" + port
	fmt.Printf("\nAGENT MONITOR running on http://127.0.0.1%s\n", addr)
	fmt.Printf("   Dashboard: http://127.0.0.1%s/\n", addr)
	if masterKey != "" || apiKeys != "" {
		fmt.Printf("   Security:  Multi-tenant Project Isolation enabled\n")
		if masterKey != "" {
			fmt.Printf("              Master Key configured for global administration\n")
		}
	} else if apiKey != "" {
		fmt.Printf("   Security:  API Key enabled (Bearer / X-API-Key / ?token=)\n")
	} else {
		fmt.Printf("   Security:  Open access (set AGENT_MONITOR_API_KEY to enable authentication)\n")
	}
	fmt.Println()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
