#!/usr/bin/env python3
import sys, os, json, subprocess, urllib.request, argparse, time, re, sqlite3

parser = argparse.ArgumentParser()
parser.add_argument("--event", default="")
parser.add_argument("--agent", default="")
parser.add_argument("--turn", type=int, default=0)
args = parser.parse_args()

raw_input = sys.stdin.read().strip() if not sys.stdin.isatty() else ""
payload = {}
if raw_input:
    try:
        payload = json.loads(raw_input)
    except Exception:
        payload = {"raw": raw_input}

# 1. 确定 Agent 名称
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

# 3. 获取会话 ID
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


def respond_and_exit(evt):
    """响应 Hook 标准协议并退出。"""
    if evt in ["beforeSubmitPrompt", "UserPromptSubmit"]:
        print(json.dumps({"continue": True}))
    elif evt in ["beforeShellExecution", "preToolUse", "PreToolUse", "PermissionRequest"]:
        print(json.dumps({"permission": "allow"}))
    else:
        print(json.dumps({}))
    sys.exit(0)


# 4. 彻底精简事件过滤：忽略无意义的底层只读工具、文件零散编辑和 PostToolUse 重复上报
tool_name = payload.get("tool_name") or payload.get("tool") or ""
is_failure = event_name in ["PostToolUseFailure", "toolFailure", "failed", "error"]

# 忽略微粒度文件修改与纯读取/管理工具
IGNORED_TOOLS = {
    "Read", "read", "read_file", "Glob", "glob", "Grep", "grep", "grep_search", "file_search",
    "TodoWrite", "TodoRead", "WebFetch", "WebSearch", "ReadSessionContext", "AskUserQuestion",
    "SendMessage", "TaskOutput", "TaskStop", "CronCreate", "CronDelete", "CronList", "CronUpdate",
    "EnterPlanMode", "ExitPlanMode", "Skill", "Edit", "Write", "edit_file", "write_file", "ApplyPatch"
}

if event_name in ["afterFileEdit"]:
    respond_and_exit(event_name)

if event_name in ["PreToolUse", "PostToolUse"]:
    if not is_failure:
        # 非失败情况下：忽略所有只读/内部工具及文件修改工具
        if tool_name in IGNORED_TOOLS:
            respond_and_exit(event_name)
        # 避免 PreToolUse 与 PostToolUse 双重重复上报，仅在 PreToolUse 记录关键执行命令
        if event_name == "PostToolUse":
            respond_and_exit(event_name)


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


def read_user_prompts(path):
    """读取并按顺序返回该会话 transcript 中的所有用户 Prompt 列表。"""
    prompts = []
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
                    prompts.append(text)
    except Exception:
        pass
    return prompts


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


def extract_turn_info():
    """提取当前轮次序号及对应 Prompt，支持 50+ 轮长会话。"""
    direct_prompt = (
        payload.get("prompt")
        or payload.get("user_prompt")
        or payload.get("user_query")
        or payload.get("user_message")
        or payload.get("task")
        or payload.get("input")
        or ""
    )
    if isinstance(direct_prompt, str) and direct_prompt.strip():
        direct_prompt = unwrap_user_query(direct_prompt)
    else:
        direct_prompt = ""

    all_prompts = []
    for path in transcript_candidates():
        prompts = read_user_prompts(path)
        if prompts:
            all_prompts = prompts
            break

    if direct_prompt:
        if not all_prompts or all_prompts[-1] != direct_prompt:
            all_prompts.append(direct_prompt)

    turn_count = max(1, len(all_prompts))
    if args.turn > 0:
        turn_count = args.turn
    current_prompt = all_prompts[-1] if all_prompts else direct_prompt
    first_prompt = all_prompts[0] if all_prompts else direct_prompt
    return turn_count, current_prompt, first_prompt


def extract_ai_response(sess_id):
    """自动从本地数据库 (ZCode SQLite) 或 Transcript 文件提取最新的 AI 回复正文。"""
    # 1. 尝试从 ZCode SQLite 数据库读取
    db_path = os.path.expanduser("~/.zcode/cli/db/db.sqlite")
    if os.path.exists(db_path):
        try:
            with sqlite3.connect(db_path) as conn:
                cur = conn.cursor()
                cur.execute(
                    "SELECT id FROM message WHERE session_id = ? AND json_extract(data, '$.role') = 'assistant' ORDER BY time_created DESC LIMIT 1;",
                    (sess_id,)
                )
                msg_row = cur.fetchone()
                if not msg_row:
                    cur.execute(
                        "SELECT id FROM message WHERE session_id LIKE ? AND json_extract(data, '$.role') = 'assistant' ORDER BY time_created DESC LIMIT 1;",
                        (f"%{sess_id}%",)
                    )
                    msg_row = cur.fetchone()
                if not msg_row:
                    cur.execute(
                        "SELECT id FROM message WHERE json_extract(data, '$.role') = 'assistant' ORDER BY time_created DESC LIMIT 1;"
                    )
                    msg_row = cur.fetchone()
                if msg_row:
                    msg_id = msg_row[0]
                    cur.execute(
                        "SELECT data FROM part WHERE message_id = ? ORDER BY time_created ASC;",
                        (msg_id,)
                    )
                    parts = cur.fetchall()
                    text_pieces = []
                    for (part_raw,) in parts:
                        try:
                            p_data = json.loads(part_raw)
                            if p_data.get("type") == "text" and p_data.get("text"):
                                text_pieces.append(p_data["text"].strip())
                        except Exception:
                            continue
                    if text_pieces:
                        return "\n\n".join(text_pieces)
        except Exception:
            pass

    # 2. 尝试从 Cursor transcripts 读取
    for path in transcript_candidates():
        try:
            if not os.path.exists(path):
                continue
            with open(path, "r", encoding="utf-8", errors="replace") as f:
                lines = f.readlines()
            for line in reversed(lines):
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                if obj.get("role") == "assistant":
                    content = obj.get("content") or obj.get("message", {}).get("content") or obj.get("text")
                    if isinstance(content, str) and content.strip():
                        return content.strip()
                    elif isinstance(content, list):
                        texts = [c.get("text", "") for c in content if isinstance(c, dict) and c.get("type") == "text"]
                        if texts:
                            return "\n\n".join(texts).strip()
        except Exception:
            pass
    return ""


def short_title(text):
    """清洗后取第一行，作短标题。"""
    if not text:
        return ""
    cleaned = text.replace("#task", "").replace("[board]", "").replace("任务:", "")
    for line in cleaned.splitlines():
        line = re.sub(r"\s+", " ", line).strip()
        if line:
            return line[:80]
    return ""


turn_index, current_prompt, first_prompt = extract_turn_info()
title = short_title(current_prompt) or short_title(first_prompt)

# 动态提取操作细节
detail = ""
tool_input = payload.get("tool_input") or payload.get("tool_args") or payload.get("parameters") or {}

if "command" in payload:
    detail = f"执行命令: {payload['command']}"
elif tool_name in ["Bash", "bash", "execute_command"]:
    cmd = tool_input.get("command") if isinstance(tool_input, dict) else ""
    detail = f"执行命令: {cmd}" if cmd else "执行命令"
elif event_name in ["beforeSubmitPrompt", "sessionStart", "SessionStart", "UserPromptSubmit"]:
    detail = "会话启动，分析任务中..."
elif event_name in ["stop", "sessionEnd", "agentCompletion", "SessionEnd", "Stop"]:
    detail = "任务执行完成"
elif is_failure:
    detail = payload.get("error") or payload.get("message") or f"工具执行失败: {tool_name}"


def get_git_info():
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

mapped_event = event_name
if event_name in ["beforeSubmitPrompt", "sessionStart", "SessionStart", "UserPromptSubmit"]:
    mapped_event = "sessionStart"
elif event_name in ["stop", "sessionEnd", "agentCompletion", "SessionEnd", "Stop"]:
    mapped_event = "agentCompletion"
elif event_name in ["PostToolUseFailure"]:
    mapped_event = "toolFailure"
elif event_name in ["error", "failed"]:
    mapped_event = "failed"
elif event_name in ["PreToolUse", "beforeShellExecution"]:
    if tool_name in ["Bash", "bash", "execute_command"] or "command" in payload:
        mapped_event = "beforeShellExecution"
    else:
        mapped_event = "toolUse"

ai_response_text = ""
if mapped_event == "agentCompletion":
    ai_response_text = extract_ai_response(session_id)
    if ai_response_text:
        ai_summary = short_title(ai_response_text)
        if ai_summary:
            detail = f"AI 回复: {ai_summary}"

data = {
    "id": session_id,
    "agent": agent_name,
    "repo": f"{repo}:{branch}",
    "event": mapped_event,
    "timestamp": int(time.time()),
    "detail": detail[:160],
    "turn_index": turn_index,
}
if title:
    data["title"] = title
if current_prompt:
    data["prompt"] = current_prompt[:4000]
if ai_response_text:
    data["ai_response"] = ai_response_text

try:
    req = urllib.request.Request(
        "http://127.0.0.1:8000/api/event",
        data=json.dumps(data).encode("utf-8"),
        headers={"Content-Type": "application/json"}
    )
    urllib.request.urlopen(req, timeout=0.8)
except Exception:
    pass

respond_and_exit(event_name)

