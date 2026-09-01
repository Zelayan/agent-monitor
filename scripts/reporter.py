#!/usr/bin/env python3
import sys, os, json, subprocess, urllib.request, argparse, time, re

parser = argparse.ArgumentParser()
parser.add_argument("--event", default="")
parser.add_argument("--agent", default="")
args = parser.parse_args()

raw_input = sys.stdin.read().strip() if not sys.stdin.isatty() else ""
payload = {}
if raw_input:
    try:
        payload = json.loads(raw_input)
    except Exception:
        payload = {"raw": raw_input}

# 1. 确定 Agent 名称（优先 CLI 参数，其次 payload/环境变量，缺省回退）
agent_name = (
    args.agent
    or payload.get("agent")
    or os.environ.get("AGENT_NAME")
    or ("ZCode" if os.environ.get("ZCODE_SESSION_ID") or "zcode" in os.environ.get("_", "").lower() else "Cursor Agent")
)

# 2. 确定 Hook 事件名
event_name = (
    args.event
    or payload.get("hook_event_name")
    or payload.get("hook_name")
    or payload.get("event")
    or "unknown"
)

# 3. 获取会话/任务 ID（兼容 ZCode、Cursor、Claude Code 等环境变量与 payload 字段）
session_id = (
    os.environ.get("ZCODE_SESSION_ID")
    or os.environ.get("CLAUDE_SESSION_ID")
    or payload.get("conversation_id")
    or payload.get("generation_id")
    or payload.get("session_id")
    or payload.get("sessionId")
    or os.environ.get("AGENT_SESSION_ID")
)
if not session_id:
    session_id = f"sess-{int(time.time())}"


def unwrap_user_query(text):
    """去掉 transcript 包装，抽出 <user_query> 正文。"""
    if not text:
        return ""
    m = re.search(r"<user_query>\s*(.*?)\s*</user_query>", text, re.DOTALL)
    if m:
        text = m.group(1)
    text = re.sub(r"<timestamp>.*?</timestamp>", "", text, flags=re.DOTALL)
    return text.strip()


def text_from_transcript_line(obj):
    if obj.get("role") != "user":
        return ""
    msg = obj.get("message") or obj
    content = msg.get("content") if isinstance(msg, dict) else obj.get("content")
    chunks = []
    if isinstance(content, list):
        for c in content:
            if isinstance(c, dict) and c.get("type") == "text":
                chunks.append(c.get("text") or "")
            elif isinstance(c, str):
                chunks.append(c)
    elif isinstance(content, str):
        chunks.append(content)
    elif isinstance(obj.get("text"), str):
        chunks.append(obj["text"])
    return unwrap_user_query("\n".join(chunks))


def read_first_user_prompt(path):
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                text = text_from_transcript_line(obj)
                if text:
                    return text
    except Exception:
        return ""
    return ""


def transcript_candidates():
    paths = []
    for key in ("transcript_path",):
        p = payload.get(key)
        if p:
            paths.append(p)
    env_p = os.environ.get("CURSOR_TRANSCRIPT_PATH")
    if env_p:
        paths.append(env_p)
    roots = []
    for env_k in ("CURSOR_PROJECT_DIR", "ZCODE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"):
        proj = os.environ.get(env_k)
        if proj:
            roots.append(proj)
    wr = payload.get("workspace_roots") or []
    if isinstance(wr, list):
        roots.extend(wr)
    home = os.path.expanduser("~")
    for root in roots:
        if not isinstance(root, str) or not root:
            continue
        slug = root.strip("/").replace("/", "-")
        base = os.path.join(home, ".cursor", "projects", slug, "agent-transcripts", session_id)
        paths.append(os.path.join(base, session_id + ".jsonl"))
        paths.append(os.path.join(base, session_id + ".txt"))
    seen = set()
    out = []
    for p in paths:
        if p and p not in seen:
            seen.add(p)
            out.append(p)
    return out


def extract_prompt():
    """首条用户 Prompt：payload 优先，否则读 transcript。"""
    raw = (
        payload.get("prompt")
        or payload.get("user_prompt")
        or payload.get("user_query")
        or payload.get("user_message")
        or payload.get("task")
        or payload.get("input")
        or ""
    )
    if isinstance(raw, str) and raw.strip():
        return unwrap_user_query(raw)
    for path in transcript_candidates():
        text = read_first_user_prompt(path)
        if text:
            return text
    return ""


def short_title(text):
    """清洗后取第一行，作卡片短标题。"""
    if not text:
        return ""
    cleaned = text.replace("#task", "").replace("[board]", "").replace("任务:", "")
    for line in cleaned.splitlines():
        line = re.sub(r"\s+", " ", line).strip()
        if line:
            return line[:80]
    return ""


# 获取 Prompt / 任务描述
prompt = extract_prompt()
title = short_title(prompt)

# 动态提取操作细节（兼容 ZCode 与 Cursor 的 tool 调用结构）
detail = ""
tool_name = payload.get("tool_name") or payload.get("tool") or ""
tool_input = payload.get("tool_input") or payload.get("tool_args") or payload.get("parameters") or {}

if event_name in ["PostToolUseFailure"]:
    err = payload.get("error") or payload.get("message") or ""
    if tool_name:
        detail = f"工具执行失败 ({tool_name}): {err}" if err else f"工具执行失败: {tool_name}"
    else:
        detail = f"工具执行失败: {err}" if err else "工具执行失败"
elif "command" in payload:
    detail = f"执行命令: {payload['command']}"
elif tool_name in ["Bash", "bash", "execute_command"]:
    cmd = tool_input.get("command") if isinstance(tool_input, dict) else ""
    detail = f"执行命令: {cmd}" if cmd else "执行命令"
elif tool_name in ["Edit", "Write", "edit_file", "write_file", "ApplyPatch"]:
    fp = tool_input.get("file_path") or tool_input.get("path") if isinstance(tool_input, dict) else ""
    detail = f"编辑文件: {fp}" if fp else f"文件操作: {tool_name}"
elif tool_name:
    detail = f"调用工具: {tool_name}"
elif "file_path" in payload:
    detail = f"编辑文件: {payload['file_path']}"
elif event_name in ["beforeSubmitPrompt", "sessionStart", "SessionStart", "UserPromptSubmit"]:
    detail = "会话启动，分析任务中..."
elif event_name in ["stop", "sessionEnd", "agentCompletion", "SessionEnd", "Stop"]:
    detail = "任务执行完成"


def get_git_info():
    """从 project_dir、workspace_roots 或当前目录解析仓库名与分支。"""
    try:
        roots = payload.get("workspace_roots")
        cwd = None
        if roots and len(roots) > 0:
            cwd = roots[0]
        else:
            cwd = os.environ.get("ZCODE_PROJECT_DIR") or os.environ.get("CURSOR_PROJECT_DIR") or os.getcwd()
        r = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=cwd, stderr=subprocess.DEVNULL).decode().strip().split("/")[-1]
        b = subprocess.check_output(["git", "branch", "--show-current"], cwd=cwd, stderr=subprocess.DEVNULL).decode().strip()
        return r, b
    except Exception:
        return "workspace", "main"

repo, branch = get_git_info()

# 统一事件名映射为 Monitor 识别的状态事件
mapped_event = event_name
if event_name in ["beforeSubmitPrompt", "sessionStart", "SessionStart", "UserPromptSubmit"]:
    mapped_event = "sessionStart"
elif event_name in ["stop", "sessionEnd", "agentCompletion", "SessionEnd", "Stop"]:
    mapped_event = "agentCompletion"
elif event_name in ["PostToolUseFailure"]:
    mapped_event = "toolFailure"
elif event_name in ["error", "failed"]:
    mapped_event = "failed"
elif event_name in ["PreToolUse", "PostToolUse"]:
    if "afterFileEdit" in event_name or tool_name in ["Edit", "Write"]:
        mapped_event = "afterFileEdit"
    elif "beforeShellExecution" in event_name or tool_name in ["Bash", "bash"]:
        mapped_event = "beforeShellExecution"
    else:
        mapped_event = "toolUse"

data = {
    "id": session_id,
    "agent": agent_name,
    "repo": f"{repo}:{branch}",
    "event": mapped_event,
    "timestamp": int(time.time()),
    "detail": detail[:120],
}
if title:
    data["title"] = title
if prompt:
    data["prompt"] = prompt[:4000]

# 异步/短超时上报至本地 AGENT MONITOR
try:
    req = urllib.request.Request(
        "http://127.0.0.1:8000/api/event",
        data=json.dumps(data).encode("utf-8"),
        headers={"Content-Type": "application/json"}
    )
    urllib.request.urlopen(req, timeout=0.8)
except Exception:
    pass

# 输出标准 hook 放行响应 (避免 stdout 格式不合规被 IDE / Client 拦截)
if event_name in ["beforeSubmitPrompt", "UserPromptSubmit"]:
    print(json.dumps({"continue": True}))
elif event_name in ["beforeShellExecution", "preToolUse", "PreToolUse", "PermissionRequest"]:
    print(json.dumps({"permission": "allow"}))
else:
    print(json.dumps({}))

sys.exit(0)
