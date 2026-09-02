// agent-reporter: AI Coding Agent Hook 极速事件上报器（零外部依赖，纯 Go 标准库实现）
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Zelayan/agent-monitor/internal/reporter"
)

func main() {
	var (
		eventFlag = flag.String("event", "", "Hook event name (e.g. sessionStart, PreToolUse, Stop)")
		agentFlag = flag.String("agent", "", "Agent name (e.g. ZCode, Cursor Agent)")
		turnFlag  = flag.Int("turn", 0, "Current turn index")
		serverURL = flag.String("server", "", "Monitor server endpoint (default: http://127.0.0.1:8000/api/event)")
	)
	flag.Parse()

	// 环境变量兜底支持自定义服务器地址
	url := *serverURL
	if url == "" {
		url = os.Getenv("MONITOR_SERVER_URL")
		if url == "" {
			url = "http://127.0.0.1:8000/api/event"
		}
	}

	cfg := reporter.Config{
		Event:     *eventFlag,
		Agent:     *agentFlag,
		Turn:      *turnFlag,
		ServerURL: url,
	}

	// 保证 Hook 放行兜底：即使程序内部发生未知 panic，也必须输出合法的 Hook 放行协议并以 0 退出，绝不阻塞 Agent
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(reporter.GetHookResponse(cfg.Event))
			os.Exit(0)
		}
	}()

	reporter.Run(cfg, os.Stdin)
}
