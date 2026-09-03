#!/usr/bin/env python3
"""
AI Code Reviewer for GitHub Pull Requests.
Zero-external-dependency script using Python standard library.
Analyzes git diffs against project-specific DDD, race-free, and fail-safe rules,
then posts or updates a structured review comment on the Pull Request.
"""

import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Dict, List, Optional, Tuple

SYSTEM_PROMPT = """你是一个世界顶级的资深 Go 语言与全栈架构师，正在对 Pull Request 进行严格、务实且高信噪比的代码审查。

请重点针对当前项目的专有架构与质量准则进行审查：

1. **DDD 架构依赖单向性**：
   - `internal/domain` 必须是纯核心业务，严禁反向依赖 `application`、`infrastructure` 或外部网络库。
2. **并发与数据隔离（Race-Free 铁律）**：
   - 共享聚合根在内存中必须受读写锁保护，外部访问或导出时必须通过深拷贝副本（如 `Task.Clone()`）。
   - 避免后台 goroutine 与实时内存对象的并发读写竞争。
3. **Hook 上报器设计（Fail-Safe 铁律）**：
   - `agent-reporter` 必须是零外部依赖（纯 Go 标准库实现）。
   - 绝不能因任何网络超时、解析错误阻断用户正常流程，必须非阻塞放行并以状态码 0 退出。
4. **前端与双语国际化（I18N）**：
   - 前端 Web 页面支持中英双语，新增用户可见文本必须在 `I18N.zh` 与 `I18N.en` 对齐，禁止硬编码未翻译的中文/英文。
   - 严禁在界面新增未经规范讨论的 Emoji 图标。
5. **Git 与安全凭据防护**：
   - 严禁硬编码密钥、Token、私有路径（如本机绝对路径）。

**输出格式要求**：
- 如果代码质量良好且没有实质性隐患，请给出简洁肯定，并总结核心变更与价值。
- 如果发现潜在 Bug、数据竞争（Data Race）、未翻译文案或架构违规，请按严重级别（Critical / Warning / Suggestion）清晰列出：
  - 问题所在文件及行号范围（如能定位）。
  - 具体原因与可能引发的后果。
  - 具体、可操作的修改建议或示例代码。
- 语言请使用简练、专业的中文。
"""

BOT_MARKER = "<!-- github-actions-ai-reviewer -->"


def http_request(
    url: str,
    method: str = "GET",
    headers: Optional[Dict[str, str]] = None,
    data: Optional[bytes] = None,
    timeout: int = 45,
) -> Tuple[int, bytes, Dict[str, str]]:
    req = urllib.request.Request(url, data=data, method=method)
    if headers:
        for k, v in headers.items():
            req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read(), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read(), dict(e.headers)
    except Exception as e:
        print(f"[Error] HTTP request failed ({url}): {e}", file=sys.stderr)
        raise


def get_pr_diff(github_token: str, repo: str, pr_number: int) -> str:
    url = f"https://api.github.com/repos/{repo}/pulls/{pr_number}"
    headers = {
        "Authorization": f"Bearer {github_token}",
        "Accept": "application/vnd.github.v3.diff",
        "User-Agent": "GitHub-Actions-AI-Reviewer",
    }
    status, body, _ = http_request(url, headers=headers)
    if status != 200:
        raise RuntimeError(f"Failed to fetch PR diff: HTTP {status} {body.decode('utf-8', errors='replace')}")
    return body.decode("utf-8", errors="replace")


def filter_diff(diff_text: str, max_chars: int = 50000) -> str:
    """Filter out binary, minified, or lock files to preserve prompt budget."""
    ignored_patterns = [
        r"package-lock\.json",
        r"go\.sum",
        r"pnpm-lock\.yaml",
        r"yarn\.lock",
        r"\.vsix$",
        r"\.tar\.gz$",
        r"dist/",
    ]
    chunks = re.split(r"(?=diff --git )", diff_text)
    retained_chunks = []

    for chunk in chunks:
        if not chunk.strip():
            continue
        first_line = chunk.splitlines()[0] if chunk.splitlines() else ""
        should_skip = any(re.search(pat, first_line) for pat in ignored_patterns)
        if should_skip:
            continue
        retained_chunks.append(chunk)

    filtered = "".join(retained_chunks)
    if len(filtered) > max_chars:
        filtered = filtered[:max_chars] + "\n\n... [Diff truncated due to size limit] ..."
    return filtered


def call_llm(
    api_key: str,
    base_url: str,
    model: str,
    pr_title: str,
    pr_body: str,
    pr_diff: str,
) -> str:
    endpoint = base_url.rstrip("/") + "/chat/completions"
    user_content = f"""请审查以下 Pull Request：

**标题**: {pr_title}
**描述**:
{pr_body or '(无描述)'}

**代码变更 (Diff)**:
```diff
{pr_diff}
```
"""
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": user_content},
        ],
        "temperature": 0.2,
    }
    data = json.dumps(payload).encode("utf-8")
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }

    status, resp_bytes, _ = http_request(endpoint, method="POST", headers=headers, data=data, timeout=90)
    if status != 200:
        raise RuntimeError(f"LLM API request failed (HTTP {status}): {resp_bytes.decode('utf-8', errors='replace')}")

    resp_json = json.loads(resp_bytes.decode("utf-8"))
    choices = resp_json.get("choices", [])
    if not choices:
        raise RuntimeError(f"No choices returned from LLM API: {resp_json}")

    return choices[0].get("message", {}).get("content", "").strip()


def find_existing_comment_id(github_token: str, repo: str, pr_number: int) -> Optional[int]:
    url = f"https://api.github.com/repos/{repo}/issues/{pr_number}/comments?per_page=100"
    headers = {
        "Authorization": f"Bearer {github_token}",
        "Accept": "application/vnd.github.v3+json",
        "User-Agent": "GitHub-Actions-AI-Reviewer",
    }
    status, body, _ = http_request(url, headers=headers)
    if status != 200:
        print(f"[Warning] Failed to list PR comments: HTTP {status}", file=sys.stderr)
        return None

    comments = json.loads(body.decode("utf-8"))
    for c in comments:
        if BOT_MARKER in c.get("body", ""):
            return c.get("id")
    return None


def post_or_update_comment(
    github_token: str,
    repo: str,
    pr_number: int,
    review_content: str,
    model: str,
) -> None:
    comment_body = (
        f"### 🤖 AI Code Review (`{model}`)\n\n"
        f"{review_content}\n\n"
        "---\n"
        f"{BOT_MARKER}\n"
        "<sub>💡 本评论由自动化 AI Review 工作流生成，遵循项目架构规范与质量准则。</sub>"
    )

    existing_id = find_existing_comment_id(github_token, repo, pr_number)
    headers = {
        "Authorization": f"Bearer {github_token}",
        "Accept": "application/vnd.github.v3+json",
        "Content-Type": "application/json",
        "User-Agent": "GitHub-Actions-AI-Reviewer",
    }
    data = json.dumps({"body": comment_body}).encode("utf-8")

    if existing_id:
        url = f"https://api.github.com/repos/{repo}/issues/comments/{existing_id}"
        method = "PATCH"
        action_desc = f"Updated existing comment #{existing_id}"
    else:
        url = f"https://api.github.com/repos/{repo}/issues/{pr_number}/comments"
        method = "POST"
        action_desc = "Posted new comment"

    status, resp_body, _ = http_request(url, method=method, headers=headers, data=data)
    if status not in (200, 210, 201):
        raise RuntimeError(f"Failed to post/update comment: HTTP {status} {resp_body.decode('utf-8', errors='replace')}")

    print(f"✓ {action_desc} on PR #{pr_number}")


def main() -> int:
    github_token = os.getenv("GITHUB_TOKEN")
    openai_api_key = os.getenv("OPENAI_API_KEY")
    base_url = os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
    model = os.getenv("OPENAI_MODEL", "gpt-4o")
    repo = os.getenv("GITHUB_REPOSITORY")
    pr_number_str = os.getenv("PR_NUMBER")
    pr_title = os.getenv("PR_TITLE", "")
    pr_body = os.getenv("PR_BODY", "")

    if not github_token:
        print("[Error] GITHUB_TOKEN is required.", file=sys.stderr)
        return 1

    if not repo or not pr_number_str:
        print("[Error] GITHUB_REPOSITORY and PR_NUMBER are required.", file=sys.stderr)
        return 1

    if not openai_api_key:
        print("[Notice] OPENAI_API_KEY is not set. Skipping AI review gracefully.")
        return 0

    pr_number = int(pr_number_str)
    print(f"==> Fetching diff for {repo} PR #{pr_number}...")
    raw_diff = get_pr_diff(github_token, repo, pr_number)
    filtered_diff = filter_diff(raw_diff)

    if not filtered_diff.strip():
        print("==> No meaningful code changes to review. Exiting.")
        return 0

    print(f"==> Calling LLM ({model} at {base_url})...")
    review_output = call_llm(
        api_key=openai_api_key,
        base_url=base_url,
        model=model,
        pr_title=pr_title,
        pr_body=pr_body,
        pr_diff=filtered_diff,
    )

    print("==> Publishing review comment to PR...")
    post_or_update_comment(github_token, repo, pr_number, review_output, model)
    return 0


if __name__ == "__main__":
    sys.exit(main())
