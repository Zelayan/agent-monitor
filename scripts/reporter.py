#!/usr/bin/env python3
import sys, os, json, subprocess, urllib.request, argparse, time, re

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


def unwrap_user_query(text):
    """去掉 Cursor transcript 包装，抽出 <user_query> 正文。"""
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
    proj = os.environ.get("CURSOR_PROJECT_DIR")
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
    raw = payload.get("prompt") or payload.get("user_message") or payload.get("task") or ""
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


# 获取 Prompt / 任务描述（有真实内容才上报 title，避免占位标题污染后续覆盖）
prompt = extract_prompt()
title = short_title(prompt)

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
    "timestamp": int(time.time()),
    "detail": detail[:120],
}
# 仅在拿到真实 Prompt 时上报 title/prompt，占位标题由 Hub 生成并可被后续升级
if title:
    data["title"] = title
if prompt:
    data["prompt"] = prompt[:4000]

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
