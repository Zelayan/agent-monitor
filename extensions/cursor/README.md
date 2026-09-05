# Agent Monitor for Cursor

Embedded real-time AI Coding Agent flight dashboard and lifecycle monitor directly inside Cursor IDE.

<p align="center">
  <img src="https://raw.githubusercontent.com/Zelayan/agent-monitor/master/docs/images/cursor-extension.png" alt="Agent Monitor embedded in the Cursor editor" width="90%">
</p>
<p align="center">
  <img src="https://raw.githubusercontent.com/Zelayan/agent-monitor/master/docs/images/cursor-extension-sidebar.png" alt="Agent Monitor sidebar and status bar timer in Cursor" width="90%">
</p>

## Features

- **In-Editor Dashboard**: View real-time Multi-Turn Run timelines, tool executions, and AI responses directly beside your code.
- **Activity Bar View**: Compact sidebar view for continuous background monitoring.
- **Live Status Bar Timer**: Instant visibility of active agent status, runtime stopwatch, and failure alarms.
- **Auto-Configured Hooks**: One-click configuration of `.cursor/hooks.json` with zero manual path troubleshooting.
- **Daemon Lifecycle Management**: Automatically detects or starts the local `agent-monitor` service.

## Settings

Open **Cursor Settings** and search for `Agent Monitor`.

| Setting | Purpose |
| --- | --- |
| `agentMonitor.llmBaseUrl` | OpenAI-compatible LLM base URL for session card titles (e.g. `http://127.0.0.1:11434/v1`) |
| `agentMonitor.llmModel` | Model name (e.g. `qwen2.5:7b` or `gpt-4o-mini`) |
| `agentMonitor.llmApiKey` | Optional API key; leave empty for local Ollama |
| `agentMonitor.llmGoalEveryN` | Refresh `goalSummary` every N completed runs (default `3`; `0` disables goal summaries) |

Leave `llmBaseUrl` or `llmModel` empty to keep heuristic titles and make **no** model requests.

If this extension started the backend, changing these settings (or `serverUrl` / `apiKey`) restarts that daemon automatically. If you already run `agent-monitor` via systemd or another process, run **Agent Monitor: Restart Backend Daemon**, or stop the external service first so the extension can launch it with the new env.

Ollama example: `llmBaseUrl` = `http://127.0.0.1:11434/v1`, `llmModel` = `qwen2.5:7b`, `llmApiKey` empty.

## Cross-Platform Embedded Binaries & Architecture Support

The VSIX distribution packages pre-compiled, self-contained binaries for the following platforms:

- **macOS Apple Silicon** (`darwin-arm64`)
- **macOS Intel** (`darwin-x64`)
- **Linux x86_64** (`linux-x64`)
- **Linux ARM64** (`linux-arm64`)
- **Windows x86_64** (`win32-x64`)

### Resolution Order & Fallbacks
When starting the background daemon or configuring workspace hooks, the extension dynamically resolves binaries using the following priority:

1. **Embedded Platform Binaries**: `bin/<platform-arch>/` inside the extension with execution permission verification (`chmod +x`) and SHA-256 integrity checks.
2. **Embedded Legacy Binaries**: `bin/` root inside the extension.
3. **Local Workspace / Repository Binaries**: `bin/` in your repository or open workspace root.
4. **System PATH**: Installed globally or placed in standard directories (`/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin`, `~/go/bin`, etc.).

### Other Operating Systems
If running on an architecture not listed above (e.g. FreeBSD, Windows ARM64, or Linux s390x), install the binaries from source:
```bash
go install github.com/Zelayan/agent-monitor@latest
go install github.com/Zelayan/agent-monitor/cmd/reporter@latest
```
Ensure they are available in your system `PATH`, and the extension will automatically discover and use them.

