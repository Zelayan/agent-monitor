#!/usr/bin/env python3
"""
AI Code Reviewer for GitHub Pull Requests & Local Workspaces.
Zero-external-dependency script using Python standard library.
Analyzes git diffs against project-specific DDD, race-free, and fail-safe rules.
Supports:
  - CI Mode: fetches PR diff and publishes/updates GitHub comments.
  - Local Mode: inspects local git diff against base branch or staged changes,
    rendering structured review findings directly in the terminal.
  - Strict Exit Mode: exits with non-zero code when critical/blocking flaws are found,
    enabling pre-PR local automated quality gates.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Dict, List, Optional, Tuple

SYSTEM_PROMPT = """你是一个世界顶级的资深 Go 语言与全栈架构师，正在对代码变更进行严格、务实且高信噪比的代码审查。

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
- 第一行必须给出明确的评级结论标签：
  - 若代码合格、没有阻断合并的严重隐患，第一行输出：`### 审查结论: [PASS]`
  - 若发现潜在 Bug、数据竞争 (Data Race)、未翻译文案或严重架构违规，第一行输出：`### 审查结论: [BLOCK]`
- 紧随其后给出核心变更总结与价值说明。
- 如果存在问题，请按严重级别（Critical / Warning / Suggestion）清晰列出：
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


def get_local_git_diff(base: str = "origin/master", staged_only: bool = False) -> Tuple[str, str]:
    """Retrieve local git diff and commit log.
    Tries base branch, falls back to local master if remote is not fetched.
    """
    if staged_only:
        cmd = ["git", "diff", "--cached"]
        title = "Local Staged Changes"
    else:
        check_base = subprocess.run(["git", "rev-parse", "--verify", base], capture_output=True, text=True)
        target_base = base
        if check_base.returncode != 0:
            target_base = "master"

        cmd = ["git", "diff", f"{target_base}...HEAD"]
        title = f"Local Branch Diff ({target_base}...HEAD)"

    diff_res = subprocess.run(cmd, capture_output=True, text=True)
    diff_text = diff_res.stdout

    if not staged_only:
        wt_diff = subprocess.run(["git", "diff"], capture_output=True, text=True).stdout
        if wt_diff.strip():
            diff_text += "\n" + wt_diff

    log_cmd = ["git", "log", "-n", "5", "--oneline"]
    log_res = subprocess.run(log_cmd, capture_output=True, text=True)
    summary = log_res.stdout.strip()

    return diff_text, f"{title}\nRecent Commits:\n{summary}"


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
    user_content = f"""请审查以下代码变更：

**标题/概要**: {pr_title}
**描述/上下文**:
{pr_body or '(无具体描述)'}

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


def load_env_defaults() -> None:
    """Load configuration from local or machine-level .env files if not already in env.
    Priority order:
      1. Existing os.environ (highest priority)
      2. Current working directory .env
      3. ~/.config/ai-reviewer/.env
      4. ~/.agent-monitor/.env
    """
    home = os.path.expanduser("~")
    candidates = [
        os.path.join(os.getcwd(), ".env"),
        os.path.join(home, ".config", "ai-reviewer", ".env"),
        os.path.join(home, ".agent-monitor", ".env"),
    ]

    for env_path in candidates:
        if not os.path.isfile(env_path):
            continue
        try:
            with open(env_path, "r", encoding="utf-8", errors="ignore") as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    key, val = line.split("=", 1)
                    key = key.strip()
                    val = val.strip().strip("'\"")
                    if key and key not in os.environ:
                        os.environ[key] = val
        except Exception:
            pass


def is_review_blocked(review_text: str) -> bool:
    """Check if the review indicates blocking issues."""
    first_lines = review_text.splitlines()[:5]
    header_text = "\n".join(first_lines).upper()
    if "[BLOCK]" in header_text or "结论: BLOCK" in header_text:
        return True
    if re.search(r"###\s*审查结论\s*:\s*\[?BLOCK\]?", header_text, re.IGNORECASE):
        return True
    return False


def parse_args():
    parser = argparse.ArgumentParser(description="AI Code Reviewer for PRs & Local Branches.")
    parser.add_argument("--local", action="store_true", help="Run in local workspace review mode instead of CI.")
    parser.add_argument("--base", default="origin/master", help="Base branch to compare against (default: origin/master).")
    parser.add_argument("--staged", action="store_true", help="Only review staged git changes.")
    parser.add_argument("--strict", action="store_true", help="Exit with non-zero exit code if review yields [BLOCK].")
    parser.add_argument("--model", default=None, help="LLM model (overrides OPENAI_MODEL env).")
    return parser.parse_args()


def main() -> int:
    load_env_defaults()
    args = parse_args()

    openai_api_key = os.getenv("OPENAI_API_KEY")
    base_url = os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
    model = args.model or os.getenv("OPENAI_MODEL", "gpt-4o")

    github_token = os.getenv("GITHUB_TOKEN")
    repo = os.getenv("GITHUB_REPOSITORY")
    pr_number_str = os.getenv("PR_NUMBER")
    pr_title = os.getenv("PR_TITLE", "")
    pr_body = os.getenv("PR_BODY", "")

    # Auto-detect mode: if --local is passed, or not in GitHub Actions PR context
    is_local = args.local or not (github_token and repo and pr_number_str)

    if not openai_api_key:
        if is_local:
            print("[Error] OPENAI_API_KEY environment variable is not set.", file=sys.stderr)
            print("Please configure ~/.config/ai-reviewer/.env or export OPENAI_API_KEY='sk-...'", file=sys.stderr)
            return 1
        else:
            print("[Notice] OPENAI_API_KEY is not set. Skipping AI review gracefully.")
            return 0

    if is_local:
        print(f"==> [Local Mode] Extracting git diff against {args.base}...")
        raw_diff, context_info = get_local_git_diff(base=args.base, staged_only=args.staged)
        filtered_diff = filter_diff(raw_diff)

        if not filtered_diff.strip():
            print("✓ No changes detected compared to base. Working directory clean.")
            return 0

        print(f"==> Reviewing changes using {model} at {base_url}...")
        review_output = call_llm(
            api_key=openai_api_key,
            base_url=base_url,
            model=model,
            pr_title="Local Workspace Changes",
            pr_body=context_info,
            pr_diff=filtered_diff,
        )

        print("\n" + "=" * 60)
        print(f"🤖 AI Code Review Findings ({model})")
        print("=" * 60 + "\n")
        print(review_output)
        print("\n" + "=" * 60)

        blocked = is_review_blocked(review_output)
        if args.strict and blocked:
            print("\n❌ Local AI Review failed: blocking issues found. Please address them before creating PR.", file=sys.stderr)
            return 1
        elif blocked:
            print("\n⚠️ Review noted blocking issues, but --strict was not specified.", file=sys.stderr)
        else:
            print("\n✓ Local AI Review passed cleanly.")
        return 0

    # CI Mode (GitHub Actions)
    if not github_token or not repo or not pr_number_str:
        print("[Error] GITHUB_TOKEN, GITHUB_REPOSITORY, and PR_NUMBER are required in CI mode.", file=sys.stderr)
        return 1

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
