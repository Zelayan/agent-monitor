// AGENT MONITOR：面向 AI Coding Agent 的多会话实时监视器。
// 基于 DDD (领域驱动设计) 分层架构构建：
// - Domain: internal/domain/task (Task 聚合根、Turn 实体、Timeline 值对象、领域仓储接口)
// - Application: internal/application/monitor (MonitorService 用例编排、SSE Hub 广播)
// - Infrastructure: internal/infrastructure/persistence (JSON 仓储实现), internal/infrastructure/transport/http (HTTP 控制器)
package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"

	"agent-monitor/internal/application/monitor"
	"agent-monitor/internal/infrastructure/persistence"
	transport "agent-monitor/internal/infrastructure/transport/http"
)

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

	// 4. 用户接口层 / HTTP 适配器：注册路由
	handler := transport.NewHandler(svc, hub, indexHTML)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	addr := ":" + port
	fmt.Printf("\nAGENT MONITOR running on http://127.0.0.1%s\n", addr)
	fmt.Printf("   Dashboard: http://127.0.0.1%s/\n\n", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
