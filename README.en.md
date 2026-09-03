<div align="center">

[English](README.en.md) | [简体中文](README.md)

# AGENT MONITOR

**A real-time flight dashboard for AI Coding Agents**

[![Release](https://img.shields.io/github/v/release/Zelayan/agent-monitor?color=10b981&label=Release&logo=github)](https://github.com/Zelayan/agent-monitor/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Zelayan/agent-monitor?color=00ADD8&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Offline Ready](https://img.shields.io/badge/Offline-100%25-emerald.svg)](#-features)
[![PWA Ready](https://img.shields.io/badge/PWA-Desktop_Install-purple.svg)](#-features)
[![CI Tests](https://img.shields.io/github/actions/workflow/status/Zelayan/agent-monitor/release.yml?label=Tests&logo=githubactions)](https://github.com/Zelayan/agent-monitor/actions)

<p align="center">
  First-class support for <b>Cursor</b>, <b>ZCode</b>, and <b>Codex (CLI / Desktop)</b>, covering the full lifecycle of AI coding assistants (other Agents are still being adapted).<br/>
  Built with the Go standard library: a single zero-dependency binary, nanosecond Fail-Safe intercepts, 100% offline embed, and a PWA desktop window.
</p>

[Quick Start](#-quick-start-30-seconds) • [Features](#-features) • [Let AI wire it up](#-let-ai-wire-it-up-for-you) • [Installation](docs/INSTALLATION.md) • [Agent Integration](docs/AGENT_INTEGRATION.md) • [Parallel Agents](docs/PARALLEL_AGENTS.en.md)

</div>

---

<details open>
  <summary><b>Preview (click to collapse / expand)</b></summary>
  <br/>
  <p align="center"><b>Cursor extension</b>: activity-bar / editor-embedded dashboard, status-bar stopwatch</p>
  <p align="center">
    <img src="docs/images/cursor-extension.png" alt="Cursor extension: Agent Monitor dashboard inside the editor" width="90%">
  </p>
  <p align="center">
    <img src="docs/images/cursor-extension-sidebar.png" alt="Cursor extension: activity-bar dashboard and status-bar stopwatch" width="90%">
  </p>
  <p align="center"><b>Web dashboard</b>: browser / PWA overview and multi-turn timeline drawer</p>
  <p align="center">
    <img src="docs/images/dashboard.png" alt="AGENT MONITOR dashboard overview" width="90%">
  </p>
  <p align="center">
    <img src="docs/images/drawer.png" alt="AGENT MONITOR drawer multi-turn timeline" width="90%">
  </p>
</details>

---

## Quick Start (30 seconds)

### 1. One-step install

#### Option A: Cursor extension (recommended; zero-config inside the IDE)
- Download the latest `agent-monitor-cursor-*.vsix` from [Releases](https://github.com/Zelayan/agent-monitor/releases);
- In Cursor press `⌘⇧P` (Windows/Linux: `Ctrl+Shift+P`) and run:
  ```text
  Extensions: Install from VSIX...
  ```
- Or from a terminal:
  ```bash
  cursor --install-extension dist/agent-monitor-cursor-1.0.1.vsix
  ```
> **Cursor extension is ready out of the box**: it starts the backend daemon, one-click workspace `.cursor/hooks.json`, a status-bar stopwatch, and an activity-bar / editor-embedded dashboard.

#### Option B: System CLI / standalone service
```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```
> Linux registers a systemd user service; macOS and Windows symlink into PATH. More options (Docker, air-gapped intranet, Go install, from source) are in the **[full installation guide](docs/INSTALLATION.md)**.

### 2. Open the dashboard
In a browser: **[http://127.0.0.1:8000/](http://127.0.0.1:8000/)**
> **PWA desktop app**: in Chrome / Edge click **Install** in the address bar to run the dashboard as a borderless desktop app, pinned to the Dock / taskbar, with its own shortcut.

---

## Features

- **Zero Dependencies**: pure Go standard library, a single static binary. No Node.js, Python, or external database.
- **Fail-Safe intercepts**: `agent-reporter` always allows the Agent to continue (`continue: true`) on any fault, timeout, or outage, and **never blocks day-to-day coding**.
- **100% offline embed**: `go:embed` ships the frontend, offline CSS/JS runtime, and local fonts inside the binary for intranet or disconnected hosts.
- **PWA desktop app**: Web App Manifest and Service Worker, installable as a frameless dark client.
- **Web Notifications**: native OS toasts when a task completes or fails; click to focus and open that task.
- **Per-project Key namespaces and auth**: optional per-project keys (`AGENT_MONITOR_API_KEYS`) plus a global admin Master Key, isolating data and operations across projects.
- **Multi-Turn Timeline**: aggregates many turns in one session, isolates duration per run, and reconstructs the tool tree (Bash, Edit, Read, and so on).
- **Short session titles (optional LLM)**: cards show a cleaned short title; with an OpenAI-compatible endpoint each run can summarize asynchronously. Session `goalSummary` refreshes every N completed runs (default 3, `AGENT_MONITOR_LLM_GOAL_EVERY_N`); the original `rootGoal` is never rewritten. Unset means no network.
- **Multi-Agent sniffing**: first-class lifecycle Hooks for **Cursor**, **ZCode**, and **Codex (CLI / Desktop)**; other Agents (Claude Code, Aider, and more) are still being adapted.

---

## Let AI wire it up for you

In any AI-driven workspace (Cursor / ZCode / Codex), send the following so the Agent can configure Hooks from [llms.txt](llms.txt):

```text
Please follow the AGENT MONITOR setup rules, read https://raw.githubusercontent.com/Zelayan/agent-monitor/main/llms.txt,
check whether agent-reporter is installed, and configure lifecycle Hook reporting for this project or the user environment.
```

---

## Integration matrix

Hook mechanics, environment variables, and tuning: **[Agent Integration Manual](docs/AGENT_INTEGRATION.md)**.

| Agent | Status | Mechanism | Config path | Minimal example |
| :--- | :--- | :--- | :--- | :--- |
| **Cursor** | Production ready | Cursor Hooks | `.cursor/hooks.json` | `{"command": "agent-reporter"}` |
| **ZCode** | Production ready | Extension Hook | `.zcode/hooks.json` | See example below |
| **Codex CLI / Desktop** | Production ready | Codex Hooks / Wrapper | `.codex/hooks.json` | `{"command": "agent-reporter"}` |
| **Claude Code** | Pending | Session Config | `~/.claude/config.json` | Planned |
| **Aider** | Pending | Terminal notify | `.aider.conf.yml` | Planned |
| **Windsurf / Trae** | Pending | Extension Hook | - | Planned |
| **Custom scripts** | REST API | HTTP POST | Any script or automation | `POST http://127.0.0.1:8000/api/event` |

<details>
<summary><b>ZCode / Cursor / Codex config examples</b></summary>

#### ZCode (`.zcode/hooks.json`)
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

#### Cursor (`.cursor/hooks.json`)
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

#### Codex (`.codex/hooks.json`)
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

## Global and project config (default filter: `#task`)

Out of the box the system **only records prompts that contain `#task`**, and ignores casual chat (once the first turn hits `#task`, later follow-ups in that session are tracked without repeating the tag). To record **everything** for a core project (no tag required):

- **Enable full capture for this project** (writes `.agent-monitor.json` in the repo root, overriding global):
  ```bash
  agent-reporter init-config --local --tag "" # force full capture in this project, no #task needed
  ```
- **Initialize global config** (optional API Key):
  ```bash
  agent-reporter init-config --tag "#task" --api-key "your-secret-token"
  ```
- **Show the effective config**:
  ```bash
  agent-reporter config
  ```

Precedence is nearest-wins: **CLI flags > environment variables (`AGENT_MONITOR_REQUIRE_TAG`, and so on) > project config (`.agent-monitor.json`) > global config (`~/.agent-monitor/config.json`)**. Details: **[Agent Integration Manual](docs/AGENT_INTEGRATION.md)**.

> **💡 Keyword Untracking & Deletion**: To stop tracking and remove a session from the board, include `#drop` or `#untrack` in your prompt. The reporter will automatically issue a delete request to Monitor and silence further events.

---

## Automated AI Code Review (GitHub Actions)

The repository includes an automated GitHub Actions AI Code Review workflow. On every Pull Request or push, it automatically reviews diffs against the project's DDD architecture, race-free concurrency rules, fail-safe hook constraints, and I18N standards.

### Configuration:
Create an Environment named `ai-review` in your GitHub repository, and configure under its **Environment secrets** (or repository-wide **Repository secrets**):
- `OPENAI_API_KEY` (Required): OpenAI API Key or compatible provider token.
- `OPENAI_BASE_URL` (Optional): Default `https://api.openai.com/v1` (supports custom proxies or reverse proxies).
- `OPENAI_MODEL` (Optional): Default `gpt-4o` (e.g. `deepseek-chat`, `gpt-4o-mini`).

---

## HTTP API

| Method | Endpoint | Purpose | Notes |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/event` | Hook event ingest | Lifecycle events from Agents (`Authorization: Bearer <key>`) |
| `GET` | `/api/stream` | SSE live stream | Server-Sent Events (`?token=<key>` or Header auth) |
| `GET` | `/api/tasks` | List all tasks | Aggregated sessions from memory / disk |
| `DELETE` | `/api/tasks` | Clear history | `?all=true` or an `ids` list |
| `GET` | `/manifest.json` | PWA manifest | App metadata and offline icons |

---

## Docs

- **[Installation (INSTALLATION.md)](docs/INSTALLATION.md)**: systemd, air-gapped packages, Docker, and builds.
- **[Agent Integration (AGENT_INTEGRATION.md)](docs/AGENT_INTEGRATION.md)**: Agent sniffing, Hook protocol, and parameters.
- **[Parallel Agents (PARALLEL_AGENTS.en.md)](docs/PARALLEL_AGENTS.en.md)**: concurrent Cursor Agents on isolated GitHub `feat/` / `fix/` branches via worktrees or Cloud Agents.
- **[LLM index (llms.txt)](llms.txt)**: high-density context for models and Coding Agents.
- **[Architecture and collaboration (AGENTS.md)](AGENTS.md)**: DDD layering, race-free locks, and Git rules.

---

## License

[MIT License](LICENSE). Issues and Pull Requests are welcome.
