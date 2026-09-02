// agent-reporter: AI Coding Agent Hook 极速事件上报器（零外部依赖，纯 Go 标准库实现）
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Zelayan/agent-monitor/internal/reporter"
)

func main() {
	// 子命令检查：支持 agent-reporter init-config 或 agent-reporter config
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init-config":
			initFs := flag.NewFlagSet("init-config", flag.ExitOnError)
			tagFlag := initFs.String("tag", "#task", "Filter tag required in prompt (e.g. #task)")
			urlFlag := initFs.String("url", "http://127.0.0.1:8000/api/event", "Monitor server URL")
			pathFlag := initFs.String("path", "", "Custom target config path")
			localFlag := initFs.Bool("local", false, "Create project-level config (.agent-monitor.json) in current directory")
			_ = initFs.Parse(os.Args[2:])

			cfg := reporter.GlobalConfig{
				RequireTag: *tagFlag,
				ServerURL:  *urlFlag,
				Disabled:   false,
			}
			target := *pathFlag
			if target == "" {
				if *localFlag {
					cwd, _ := os.Getwd()
					target = filepath.Join(cwd, ".agent-monitor.json")
				} else {
					target = reporter.DefaultConfigPath()
				}
			}
			if err := reporter.WriteDefaultConfigFile(cfg, target); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating config file %s: %v\n", target, err)
				os.Exit(1)
			}
			scope := "global"
			if *localFlag || target != reporter.DefaultConfigPath() {
				scope = "project"
			}
			fmt.Printf("✓ Successfully created %s config: %s\n", scope, target)
			fmt.Printf("  Filter Tag: %s (only prompts containing %q will be monitored)\n", cfg.RequireTag, cfg.RequireTag)
			fmt.Printf("  Server URL: %s\n", cfg.ServerURL)
			return

		case "config":
			cwd, _ := os.Getwd()
			projPath := reporter.FindProjectConfigFile(cwd)
			globalPath := reporter.DefaultConfigPath()
			effectiveCfg := reporter.LoadConfigForWorkspace(cwd)

			fmt.Printf("Effective Config for current directory (%s):\n", cwd)
			if projPath != "" {
				fmt.Printf("  Project Config: %s (Active)\n", projPath)
			} else {
				fmt.Printf("  Project Config: none\n")
			}
			fmt.Printf("  Global Config:  %s\n", globalPath)
			fmt.Printf("  Require Tag:    %q\n", effectiveCfg.RequireTag)
			fmt.Printf("  Server URL:     %q\n", effectiveCfg.ServerURL)
			fmt.Printf("  Disabled:       %v\n", effectiveCfg.Disabled)
			return
		}
	}

	var (
		eventFlag      = flag.String("event", "", "Hook event name (e.g. sessionStart, PreToolUse, Stop)")
		agentFlag      = flag.String("agent", "", "Agent name (e.g. ZCode, Cursor Agent)")
		turnFlag       = flag.Int("turn", 0, "Current turn index")
		serverURL      = flag.String("server", "", "Monitor server endpoint (default: http://127.0.0.1:8000/api/event)")
		requireTagFlag = flag.String("require-tag", "", "Filter tag required in prompt (e.g. #task)")
	)
	flag.Parse()

	cfg := reporter.Config{
		Event:      *eventFlag,
		Agent:      *agentFlag,
		Turn:       *turnFlag,
		ServerURL:  *serverURL,
		RequireTag: *requireTagFlag,
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
