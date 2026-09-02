<div align="center">

# AGENT MONITOR

**面向 AI Coding Agent 的专业级实时飞行仪表盘**

[![Release](https://img.shields.io/github/v/release/Zelayan/agent-monitor?color=10b981&label=Release&logo=github)](https://github.com/Zelayan/agent-monitor/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Zelayan/agent-monitor?color=00ADD8&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Offline Ready](https://img.shields.io/badge/Offline-100%25-emerald.svg)](#-核心特性)
[![PWA Ready](https://img.shields.io/badge/PWA-Desktop_Install-purple.svg)](#-核心特性)
[![CI Tests](https://img.shields.io/github/actions/workflow/status/Zelayan/agent-monitor/release.yml?label=Tests&logo=githubactions)](https://github.com/Zelayan/agent-monitor/actions)

<p align="center">
  统一监控 <b>Claude Code</b>、<b>Cursor</b>、<b>ZCode</b>、<b>Aider</b>、<b>Windsurf</b> 等各类 AI 编程助手的执行全生命周期。<br/>
  基于 Go 标准库构建，0 外部依赖单二进制，纳秒级 Fail-Safe 拦截，100% 离线内嵌运行，支持 PWA 原生桌面窗口。
</p>

[⚡️ 快速开始](#️-快速开始-30-秒上手) • [✨ 核心特性](#-核心特性) • [🤖 让 AI 自动接入](#-让-ai-自动为你接入) • [📖 安装指南](docs/INSTALLATION.md) • [🔌 Agent 集成手册](docs/AGENT_INTEGRATION.md)

</div>

---

<details open>
  <summary><b>🖼️ 仪表盘视觉预览（点击折叠 / 展开）</b></summary>
  <br/>
  <p align="center">
    <img src="docs/images/dashboard.png" alt="AGENT MONITOR 看板全览" width="90%">
  </p>
  <p align="center">
    <img src="docs/images/drawer.png" alt="AGENT MONITOR 抽屉多轮时间线" width="90%">
  </p>
</details>

---

## ⚡️ 快速开始 (30 秒上手)

### 1. 一键安装
```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```
> Linux 自动注册 systemd 用户服务守护；macOS 与 Windows 自动软链至 PATH。更多安装方式（Docker、离线内网、Go Install、源码编译）详见 **[📖 完整安装与部署指南](docs/INSTALLATION.md)**。

### 2. 访问看板
打开浏览器访问：**[http://127.0.0.1:8000/](http://127.0.0.1:8000/)**
> 💡 **PWA 原生桌面体验**：在 Chrome / Edge 地址栏右侧点击 **「安装」**，即可将看板作为无边框独立桌面 App 运行，常驻 Dock / 任务栏，支持快捷键独立切换。

---

## ✨ 核心特性

- **🚀 极致轻量与零依赖（Zero Dependencies）**：纯 Go 标准库实现，单静态二进制部署，无需 Node.js、Python 或外部数据库。
- **🛡️ 毫秒级 Fail-Safe 拦截铁律**：`agent-reporter` 拦截器遇任何故障、网络超时或宕机均在毫秒内静默放行（`continue: true`），**绝不阻断日常编码**。
- **🌐 100% 离线内嵌**：通过 `go:embed` 将前端页面、离线样式、JS 运行时与本地字体完整编译进二进制，内网或脱机环境开箱即用。
- **🖥️ PWA 桌面独立应用**：内置 Web App Manifest 与 Service Worker，支持桌面安装为无浏览器边框的沉浸式黑底客户端。
- **🔔 桌面原生通知（Web Notifications）**：任务从运行到完成或异常中断时向操作系统发送原生桌面弹窗，点击即可快速唤起并展开该任务详情。
- **🔒 API Key 访问控制**：支持通过 `AGENT_MONITOR_API_KEY`（或 `--api-key`）开启轻量鉴权，防范公网/局域网未授权写入与误清空。
- **⏱️ 多轮 Run 会话矩阵（Multi-Turn Timeline）**：自动聚合单会话内的多轮交互，按轮次隔离耗时，清晰还原工具调用（Bash、Edit、Read 等）的完整树状轨迹。
- **🔍 智能多 Agent 动态嗅探**：通过会话上下文与环境变量自适应推断来源（Claude Code、Cursor、ZCode、Aider、Trae 等），无需繁琐手工配置。

---

## 🤖 让 AI 自动为你接入

在任何由 AI 驱动的项目工作区（如 Claude Code / Cursor / ZCode / Aider）中，直接发送以下指令，AI 即可根据本项目预置的 [llms.txt](llms.txt) 和配置指南自动完成 Hook 接入：

```text
请参考当前系统中的 AGENT MONITOR 配置规范，阅读 https://raw.githubusercontent.com/Zelayan/agent-monitor/main/llms.txt，
检查本机是否已安装 agent-reporter，并为当前项目或全局环境配置好生命周期 Hook 上报。
```

---

## 🔌 接入支持一览

详细的 Hook 机制、环境变量支持与参数调优详见 **[🔌 Agent 集成与配置手册](docs/AGENT_INTEGRATION.md)**。

| Agent | 接入机制 | 配置文件路径 | 最小配置示例 |
| :--- | :--- | :--- | :--- |
| **ZCode** | 扩展 Hook | `.zcode/hooks.json` | 见下方配置示例 |
| **Cursor** | Cursor Hooks | `.cursor/hooks.json` | `{"command": "agent-reporter"}` |
| **Claude Code** | Session Config | `~/.claude/config.json` | 配置 `toolHooks` / `eventHooks` |
| **Aider** | 终端通知 | `.aider.conf.yml` | `notification-command: "agent-reporter"` |
| **自定义 / Continue** | REST API | 任何脚本或自动化 | `POST http://127.0.0.1:8000/api/event` |

<details>
<summary><b>展开查看 ZCode / Cursor 配置示例</b></summary>

#### ZCode 配置 (`.zcode/hooks.json`)
```json
{
  "hooks": {
    "PostToolUse": [{ "command": "agent-reporter" }],
    "PostToolUseFailure": [{ "command": "agent-reporter" }],
    "SessionStart": [{ "command": "agent-reporter" }],
    "UserPromptSubmit": [{ "command": "agent-reporter" }],
    "Stop": [{ "command": "agent-reporter" }]
  }
}
```

#### Cursor 配置 (`.cursor/hooks.json`)
```json
{
  "version": 1,
  "hooks": {
    "beforeSubmitPrompt": [{ "command": "agent-reporter" }],
    "afterAgentResponse": [{ "command": "agent-reporter" }],
    "stop": [{ "command": "agent-reporter" }]
  }
}
```
</details>

---

## 🎯 全局与项目级配置（支持精准标签过滤）

如果你希望**全局默认只监控重要任务（如带有 `#task` 的 Prompt），忽略日常随意闲聊**，或者针对**核心业务项目开启全量追踪**：

- **初始化全局配置（可同时配置 API Key）**：
  ```bash
  agent-reporter init-config --tag "#task" --api-key "your-secret-token"
  ```
- **针对当前项目定制覆盖**（在当前项目根目录生成 `.agent-monitor.json`）：
  ```bash
  agent-reporter init-config --local --tag "" # 当前项目强制全量监控，无需打 #task
  ```
- **查看当前生效配置**：
  ```bash
  agent-reporter config
  ```

配置优先级遵循就近原则：**命令行参数 > 环境变量 (`AGENT_MONITOR_API_KEY` 等) > 项目配置 (`.agent-monitor.json`) > 全局配置 (`~/.agent-monitor/config.json`)**。详细说明见 **[🔌 Agent 集成与配置手册](docs/AGENT_INTEGRATION.md)**。

---

## 📡 HTTP API 概览

| Method | Endpoint | 用途 | 说明 |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/event` | Hook 事件上报 | 接收各 Agent 触发的生命周期事件（支持 `Authorization: Bearer <key>`） |
| `GET` | `/api/stream` | SSE 实时事件流 | Server-Sent Events（支持 `?token=<key>` 或 Header 鉴权） |
| `GET` | `/api/tasks` | 获取所有任务 | 返回当前内存/磁盘上的全量会话聚合数据 |
| `DELETE` | `/api/tasks` | 清空历史任务 | 支持 `?all=true` 或指定 `ids` 列表批量删除 |
| `GET` | `/manifest.json` | PWA 清单 | 提供应用元数据与离线图标描述 |

---

## 📚 文档导航

- **[📖 安装与部署指南 (INSTALLATION.md)](docs/INSTALLATION.md)**：包含 systemd 守护管理、内网离线包、Docker 镜像与编译构建全指南。
- **[🔌 Agent 集成手册 (AGENT_INTEGRATION.md)](docs/AGENT_INTEGRATION.md)**：各 Agent 嗅探规则、Hook 协议定义与参数说明。
- **[🤖 LLM 索引接口 (llms.txt)](llms.txt)**：专为大模型与 Coding Agent 设计的高密度上下文接入入口。
- **[📐 架构与协作规范 (AGENTS.md)](AGENTS.md)**：DDD 分层规范、并发安全读写锁机制与 Git 铁律。

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 开源。欢迎提 Issue 与 Pull Request！
