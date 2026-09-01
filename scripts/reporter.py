#!/usr/bin/env python3
import sys, os, json, subprocess, urllib.request, argparse, time

parser = argparse.ArgumentParser()
parser.add_argument("--event", default="")
parser.add_argument("--agent", default="Cursor Agent")
args = parser.parse_args()

raw_input = sys.stdin.read().strip() if not sys.stdin.isatty() else ""
payload = {}
if raw_input:
    try:
        payload = json.loads(raw_input)
    except Exception:
        payload = {"raw": raw_input}

# 优先从 payload 中获取 Cursor 官方传递的 hook_event_name
event_name = args.event or payload.get("hook_event_name") or "unknown"

# 获取会话/任务 ID
session_id = (
    payload.get("conversation_id")
    or payload.get("generation_id")
    or payload.get("session_id")
    or payload.get("sessionId")
    or os.environ.get("AGENT_SESSION_ID")
)
if not session_id:
    session_id = f"sess-{int(time.time())}"

# 获取 Prompt / 任务描述
prompt = (
    payload.get("prompt")
    or payload.get("user_message")
    or payload.get("task")
    or ""
)

# 动态提取操作细节
detail = ""
if "command" in payload:
    detail = f"执行命令: {payload['command']}"
elif "tool_name" in payload:
    tool_input = payload.get("tool_input", {})
    if isinstance(tool_input, dict) and "command" in tool_input:
        detail = f"执行命令: {tool_input['command']}"
    elif isinstance(tool_input, dict) and "path" in tool_input:
        detail = f"操作文件: {tool_input['path']}"
    else:
        detail = f"调用工具: {payload['tool_name']}"
elif "file_path" in payload:
    detail = f"编辑文件: {payload['file_path']}"
elif event_name in ["beforeSubmitPrompt", "sessionStart"]:
    detail = "会话启动，分析任务中..."
elif event_name in ["stop", "sessionEnd", "agentCompletion"]:
    detail = "任务执行完成"

def get_git_info():
    """从 workspace_roots 或当前目录解析仓库名与分支。"""
    try:
        roots = payload.get("workspace_roots")
        cwd = roots[0] if roots and len(roots) > 0 else os.getcwd()
        r = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=cwd, stderr=subprocess.DEVNULL).decode().strip().split("/")[-1]
        b = subprocess.check_output(["git", "branch", "--show-current"], cwd=cwd, stderr=subprocess.DEVNULL).decode().strip()
        return r, b
    except Exception:
        return "workspace", "main"

repo, branch = get_git_info()

title = prompt.replace("#task", "").replace("[board]", "").replace("任务:", "").strip()
if not title:
    title = f"{args.agent} 任务"

# 统一事件名映射为看板识别的状态事件
mapped_event = event_name
if event_name in ["beforeSubmitPrompt", "sessionStart"]:
    mapped_event = "sessionStart"
elif event_name in ["stop", "sessionEnd", "agentCompletion"]:
    mapped_event = "agentCompletion"

data = {
    "id": session_id,
    "agent": args.agent,
    "repo": f"{repo}:{branch}",
    "event": mapped_event,
    "title": title[:80],
    "timestamp": int(time.time()),
    "detail": detail[:120],
}

# 异步/短超时上报至本地 Kanban Hub
try:
    req = urllib.request.Request(
        "http://127.0.0.1:8000/api/event",
        data=json.dumps(data).encode("utf-8"),
        headers={"Content-Type": "application/json"}
    )
    urllib.request.urlopen(req, timeout=0.8)
except Exception:
    pass

# 输出标准 hook 放行响应 (避免 stdout 格式不合规被 IDE 拦截)
if event_name == "beforeSubmitPrompt":
    print(json.dumps({"continue": True}))
elif event_name in ["beforeShellExecution", "preToolUse"]:
    print(json.dumps({"permission": "allow"}))
else:
    print(json.dumps({}))

sys.exit(0)
