# Agent Integration Manual (For AI Coding Agents)

This document is specifically structured for **AI Coding Agents** (such as Claude Code, Cursor Agent, ZCode, Aider, Windsurf, Trae, Continue) to automatically inspect their host environment and self-configure lifecycle hooks to report session events into **Agent Monitor**.

---

## 1. Goal & Requirements
When a user asks you to *"integrate Agent Monitor"* or *"configure monitoring hooks"*, follow the steps below sequentially:
1. Ensure the `agent-reporter` CLI binary is installed and executable in PATH.
2. Determine which AI Coding Agent you currently are.
3. Automatically create or update the appropriate workspace/global hook configuration file.
4. Send a test ping to verify connectivity with `http://127.0.0.1:8000/api/event`.

---

## 2. Step 1: Ensure `agent-reporter` is Available

Run the following command to check if `agent-reporter` is installed:

```bash
which agent-reporter || echo "NOT_FOUND"
```

If `agent-reporter` is not found, install it automatically by executing:

```bash
curl -fsSL https://raw.githubusercontent.com/Zelayan/agent-monitor/main/install.sh | bash
```

On Linux this also enables the `agent-monitor` systemd user service. If `http://127.0.0.1:8000/` is down, start it with `systemctl --user start agent-monitor` (or run `agent-monitor` in the foreground). Do not wrap the install script in `sudo`.

Air-gapped / intranet hosts: do not curl GitHub. Copy `agent-monitor_*_linux-offline.tar.gz`, extract it, `cd` into the directory and run `./install.sh` (no arguments, no network). Passing the archive path also works: `./install.sh ./agent-monitor_*_linux-offline.tar.gz`.

Alternatively, if Go is installed:
```bash
go install github.com/Zelayan/agent-monitor/cmd/reporter@latest
```

---

## 3. Step 2: Identify Your Agent Environment

Inspect your active environment variables to determine your agent type:
- If `CLAUDE_SESSION_ID` or `CLAUDE_PROJECT_DIR` is set $\rightarrow$ **Claude Code**
- If `ZCODE_SESSION_ID` or `ZCODE_PROJECT_DIR` is set $\rightarrow$ **ZCode**
- If `CURSOR_PROJECT_DIR` or `.cursor/` exists $\rightarrow$ **Cursor Agent**
- If `AIDER_SESSION_ID` or running inside Aider $\rightarrow$ **Aider**
- If `WINDSURF_PROJECT_DIR` or `CODEIUM_PROJECT_DIR` is set $\rightarrow$ **Windsurf**
- If `TRAE_PROJECT_DIR` is set $\rightarrow$ **Trae**
- Otherwise $\rightarrow$ **Custom / General Agent**

---

## 4. Step 3: Self-Configuration Recipes

### Recipe A: If you are Claude Code
Create or merge into `.claude/config.json` (or `~/.claude/config.json`):

```json
{
  "hooks": {
    "enabled": true,
    "events": {
      "SessionStart": [
        { "hooks": [{ "type": "command", "command": "agent-reporter --event SessionStart --agent 'Claude Code'" }] }
      ],
      "UserPromptSubmit": [
        { "hooks": [{ "type": "command", "command": "agent-reporter --event UserPromptSubmit --agent 'Claude Code'" }] }
      ],
      "PreToolUse": [
        { "matcher": ".*", "hooks": [{ "type": "command", "command": "agent-reporter --event PreToolUse --agent 'Claude Code'" }] }
      ],
      "PostToolUseFailure": [
        { "matcher": ".*", "hooks": [{ "type": "command", "command": "agent-reporter --event PostToolUseFailure --agent 'Claude Code'" }] }
      ],
      "Stop": [
        { "hooks": [{ "type": "command", "command": "agent-reporter --event Stop --agent 'Claude Code'" }] }
      ]
    }
  }
}
```

### Recipe B: If you are Cursor Agent
Create or merge into `.cursor/hooks.json` (or `~/.cursor/hooks.json`):

```json
{
  "version": 1,
  "hooks": {
    "sessionStart": [{ "command": "agent-reporter --event sessionStart --agent 'Cursor Agent'" }],
    "beforeSubmitPrompt": [{ "command": "agent-reporter --event beforeSubmitPrompt --agent 'Cursor Agent'" }],
    "preToolUse": [{ "command": "agent-reporter --event preToolUse --agent 'Cursor Agent'" }],
    "postToolUseFailure": [{ "command": "agent-reporter --event postToolUseFailure --agent 'Cursor Agent'" }],
    "beforeShellExecution": [{ "command": "agent-reporter --event beforeShellExecution --agent 'Cursor Agent'" }],
    "beforeMCPExecution": [{ "command": "agent-reporter --event beforeMCPExecution --agent 'Cursor Agent'" }],
    "subagentStart": [{ "command": "agent-reporter --event subagentStart --agent 'Cursor Agent'" }],
    "afterAgentResponse": [{ "command": "agent-reporter --event afterAgentResponse --agent 'Cursor Agent'" }],
    "stop": [{ "command": "agent-reporter --event stop --agent 'Cursor Agent'" }],
    "sessionEnd": [{ "command": "agent-reporter --event sessionEnd --agent 'Cursor Agent'" }]
  }
}
```

Use official Cursor hook names (`stop`, not `agentCompletion`). Project hooks run from the repo root; user hooks (`~/.cursor/hooks.json`) should use an absolute path to `agent-reporter`. Do not install the same events at both project and user level, or each event will fire twice.

### Recipe C: If you are ZCode
Create or merge into `.zcode/config.json`:

```json
{
  "hooks": {
    "enabled": true,
    "events": {
      "SessionStart": [
        { "hooks": [{ "type": "command", "command": "agent-reporter --event SessionStart --agent ZCode" }] }
      ],
      "UserPromptSubmit": [
        { "hooks": [{ "type": "command", "command": "agent-reporter --event UserPromptSubmit --agent ZCode" }] }
      ],
      "PreToolUse": [
        { "matcher": ".*", "hooks": [{ "type": "command", "command": "agent-reporter --event PreToolUse --agent ZCode" }] }
      ],
      "PostToolUseFailure": [
        { "matcher": ".*", "hooks": [{ "type": "command", "command": "agent-reporter --event PostToolUseFailure --agent ZCode" }] }
      ],
      "Stop": [
        { "hooks": [{ "type": "command", "command": "agent-reporter --event Stop --agent ZCode" }] }
      ]
    }
  }
}
```

### Recipe D: If you are Aider
Create or merge into `.aider.conf.yml`:

```yaml
notifications-command: "agent-reporter --event agentCompletion --agent Aider"
auto-commits: true
```

### Recipe E: If you are Continue
Add to `~/.continue/config.json`:

```json
{
  "analytics": {
    "provider": "custom",
    "url": "http://127.0.0.1:8000/api/event"
  }
}
```

---

## 5. Step 4: Verify Integration

Run a quick test ping to ensure the local Agent Monitor server is listening:

```bash
curl -s -X POST http://127.0.0.1:8000/api/event \
  -H "Content-Type: application/json" \
  -d '{"id":"ping-test","agent":"Test Agent","event":"sessionStart","title":"Connection Test","detail":"Ready"}'
```

If the response is `{"status":"ok"}`, integration is complete and active! Clean up the test task:

```bash
curl -s -X DELETE 'http://127.0.0.1:8000/api/tasks?ids=ping-test'
```

---

## 6. Global & Project-Level Configuration & Session Filtering

Instead of exporting environment variables in multiple shells or editing individual workspace configs, you can manage reporting behavior through unified configuration files:

- **Global Config**: `~/.agent-monitor/config.json` (User-wide default baseline)
- **Project-Level Config**: `<project-root>/.agent-monitor.json` (Overrides global settings for the specific workspace)

### Quick Setup:
```bash
# 1. Initialize global configuration (e.g. require #task by default across all projects)
agent-reporter init-config --tag "#task"

# 2. Or initialize project-level override (.agent-monitor.json in current directory)
agent-reporter init-config --local --tag "" # monitor all sessions without tag in this project

# 3. View currently effective configuration for the current directory
agent-reporter config
```

### Configuration Format (`~/.agent-monitor/config.json` or `<project>/.agent-monitor.json`):
```json
{
  "require_tag": "#task",
  "server_url": "http://127.0.0.1:8000/api/event",
  "api_key": "your-secret-api-key",
  "disabled": false
}
```

### Priority Hierarchy:
1. **CLI Flag**: `--require-tag` / `--server` / `--api-key`
2. **Environment Variables**: `AGENT_MONITOR_REQUIRE_TAG` / `AGENT_MONITOR_URL` / `AGENT_MONITOR_API_KEY`
3. **Project Config**: `<workspace>/.agent-monitor.json`
4. **Global Config**: `~/.agent-monitor/config.json`

### Key Options:
- **`require_tag`**: e.g. `"#task"` (or comma-separated `"#task,#todo"`).
  - Only prompts containing the tag will be reported to Agent Monitor.
  - Set to `""` in project-level `.agent-monitor.json` to force monitoring all sessions in that specific workspace.
- **`server_url`**: Set a local or remote monitor server URL (e.g. `"http://192.168.1.100:8000/api/event"`).
- **`api_key`**: Secret token for authenticating events sent to protected monitor servers.
- **`disabled`**: Temporarily pause monitoring without removing any hooks (`true` / `false`).

