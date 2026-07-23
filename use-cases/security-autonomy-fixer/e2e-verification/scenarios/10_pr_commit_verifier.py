#!/usr/bin/env python3
"""
Scenario 10 — PR Commit Verifier: Check Flowgent's Fix Commits on Target PR

Validates that the security-autonomy-fixer L2 agents produced actual fix commits
on the target repository's pull request. This is the "work product" verification —
it checks what the agents delivered, not just whether the pipeline ran.

PR under test: https://github.com/wl4g/rengine/pull/4
Branch: fix/flowgent_sec_auto_fix

Verification Strategy:
1. Query GitHub API for PR metadata and commit list
2. Verify the PR exists and is open/merged
3. Count commits beyond the original PR creation
4. Look for Flowgent-authored commits (commit message patterns)
5. Check for test file changes (coverage increase)
6. Report file-level diff statistics
"""

import os
import sys
import json
import requests

sys.path.insert(0, '..')
import config

GITHUB_API = "https://api.github.com"
REPO = "wl4g/rengine"
PR_NUMBER = 4
BRANCH = "fix/flowgent_sec_auto_fix"

GITHUB_TOKEN = os.getenv("GITHUB_TOKEN") or os.getenv("GH_TOKEN") or ""
HEADERS = {
    "Accept": "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
}
if GITHUB_TOKEN:
    HEADERS["Authorization"] = f"Bearer {GITHUB_TOKEN}"

FLOWGENT_SECURITY_FIX_PATTERNS = [
    "fix: resolve sonarqube",
    "fix: sonarqube",
    "fix: security",
    "fix: remediate",
    "fix: sql injection",
    "fix: xss",
    "fix: csrf",
    "fix: vulnerability",
    "security-autonomy-fixer",
]

FLOWGENT_CI_PATTERNS = [
    "quality gate",
    "sonar check",
    "sonarqube workflow",
    "ci workflow",
    "ci:",
]


def _gh_get(path: str) -> dict:
    url = f"{GITHUB_API}{path}"
    r = requests.get(url, headers=HEADERS, timeout=15)
    if r.status_code != 200:
        print(f"  FAIL: GitHub API returned {r.status_code} for {path}: {r.text[:200]}")
        return None
    return r.json()


def run():
    print("\n" + "=" * 60)
    print("  Scenario 12: PR Commits — Verify Fix Commits on Target PR")
    print("=" * 60)

    # 1. Get PR metadata
    print(f"\n  -> Querying PR #{PR_NUMBER} on {REPO}...")
    pr = _gh_get(f"/repos/{REPO}/pulls/{PR_NUMBER}")
    if pr is None:
        raise AssertionError(
            f"Cannot access GitHub API for PR #{PR_NUMBER}. "
            f"Ensure GITHUB_TOKEN is set and has repo scope. "
            f"Without API access, commit verification is impossible."
        )

    pr_title = pr.get("title", "?")
    pr_state = pr.get("state", "?")
    pr_url = pr.get("html_url", "")
    created_at = pr.get("created_at", "?")
    updated_at = pr.get("updated_at", "?")
    files_changed = pr.get("changed_files", 0)
    additions = pr.get("additions", 0)
    deletions = pr.get("deletions", 0)

    print(f"  PR Title: {pr_title}")
    print(f"  State: {pr_state}")
    print(f"  Created: {created_at}")
    print(f"  Updated: {updated_at}")
    print(f"  Files changed: {files_changed}  +{additions}/-{deletions}")

    # 2. Get commits on the PR (only those unique to the head branch vs base)
    print(f"\n  -> Fetching commits on PR #{PR_NUMBER}...")
    commits_data = _gh_get(f"/repos/{REPO}/pulls/{PR_NUMBER}/commits?per_page=100")
    if commits_data is None:
        raise AssertionError(f"Cannot fetch commits for branch '{BRANCH}' — GitHub API unreachable")
    if not isinstance(commits_data, list):
        raise AssertionError(f"Unexpected commits response type: {type(commits_data).__name__}")
    commits = commits_data

    print(f"  Total commits on PR: {len(commits)}")

    # 3. Classify commits: security fixes vs CI/workflow vs other
    flowgent_security_fixes = []
    flowgent_ci_commits = []
    other_commits = []

    for c in commits:
        commit_data = c.get("commit", {})
        msg_full = commit_data.get("message", "")
        msg = msg_full.lower()
        author = commit_data.get("author", {}).get("name", "")
        committer = commit_data.get("committer", {}).get("name", "")
        sha = c.get("sha", "")[:8]

        is_security_fix = any(pattern in msg for pattern in FLOWGENT_SECURITY_FIX_PATTERNS)
        is_ci = any(pattern in msg for pattern in FLOWGENT_CI_PATTERNS)
        is_flowgent_author = "flowgent" in author.lower() or "flowgent" in committer.lower()

        entry = {
            "sha": sha,
            "message": msg_full.split("\n")[0],
            "author": author,
            "committer": committer,
        }

        if is_security_fix and is_flowgent_author:
            flowgent_security_fixes.append(entry)
        elif is_ci and is_flowgent_author:
            flowgent_ci_commits.append(entry)
        else:
            other_commits.append(entry)

    print(f"  Flowgent security-fix commits: {len(flowgent_security_fixes)}")
    for fc in flowgent_security_fixes:
        print(f"    {fc['sha']} {fc['message'][:80]} (author: {fc['author']})")
    if flowgent_security_fixes:
        print(f"  ✓ These are commits produced by the security-autonomy-fixer pipeline")

    print(f"  Flowgent CI/workflow commits (NOT security fixes): {len(flowgent_ci_commits)}")
    for fc in flowgent_ci_commits:
        print(f"    {fc['sha']} {fc['message'][:80]} (author: {fc['author']})")

    print(f"  Other commits: {len(other_commits)}")
    for oc in other_commits[:5]:
        print(f"    {oc['sha']} {oc['message'][:80]} (author: {oc['author']})")

    # 4. Check files in PR for test coverage
    print(f"\n  -> Checking PR files for test coverage changes...")
    files_data = _gh_get(f"/repos/{REPO}/pulls/{PR_NUMBER}/files?per_page=100")
    if isinstance(files_data, list):
        test_files = []
        src_files = []
        for f in files_data:
            fn = f.get("filename", "")
            if "test" in fn.lower() or fn.endswith("_test.go") or fn.endswith("Test.java"):
                test_files.append(fn)
            else:
                src_files.append(fn)

        print(f"  Source files changed: {len(src_files)}")
        for sf in src_files:
            print(f"    {sf} (+{sum(f.get('additions', 0) for f in files_data if f.get('filename') == sf)}/-{sum(f.get('deletions', 0) for f in files_data if f.get('filename') == sf)})")
        print(f"  Test files changed: {len(test_files)}")
        for tf in test_files:
            print(f"    {tf}")
    else:
        test_files = []
        src_files = []

    # 5. Verdict
    print(f"\n  {'=' * 60}")
    checks = []

    # Critical: PR must be accessible
    checks.append(("PR accessible", pr_state in ("open", "merged", "closed"), True))

    # Critical: Branch must have commits
    checks.append(("PR has commits", len(commits) > 0, True))

    # Critical: Flowgent security-autonomy-fixer must have produced fix commits
    # (NOT CI/workflow commits — those are NOT security fixes)
    has_security_fixes = len(flowgent_security_fixes) > 0
    checks.append(("Flowgent security-fix commits on PR", has_security_fixes, True))

    # Critical: Fix commits must touch source code, not just CI configs
    source_files_changed = [fn for fn in src_files if not fn.startswith(".github/")]
    checks.append(("Source code changes (not just CI)", len(source_files_changed) > 0, True))

    # Non-critical: test coverage changes
    has_tests = len(test_files) > 0
    checks.append(("Test coverage changes", has_tests, False))

    for label, ok, critical in checks:
        marker = "OK" if ok else ("FAIL" if critical else "WARN")
        print(f"  [{marker}] {label}: {'yes' if ok else 'no/missing'}")

    if flowgent_ci_commits and not flowgent_security_fixes:
        print(f"\n  ⚠  Found {len(flowgent_ci_commits)} Flowgent CI/workflow commit(s) but ZERO security-fix commits.")
        print(f"  ⚠  The security-autonomy-fixer pipeline has NOT produced any code fixes.")

    critical_failures = [label for label, ok, critical in checks if not ok and critical]
    if critical_failures:
        raise AssertionError(
            f"Critical PR checks failed: {critical_failures}. "
            f"Flowgent security-autonomy-fixer has NOT pushed fix commits to {pr_url}. "
            f"Check: (1) GitHub MCP token has repo:write scope, "
            f"(2) commit-fixes and create-pr nodes completed successfully, "
            f"(3) MCP tool names in flow definition match the real GitHub MCP server tools."
        )

    if has_security_fixes:
        print(f"\n  ✓ Flowgent security-autonomy-fixer produced {len(flowgent_security_fixes)} commit(s)")
    if test_files:
        print(f"  ✓ Test coverage changes detected in {len(test_files)} file(s)")
    else:
        print(f"  ⚠  No test file changes — security fixes should include test coverage")
    print(f"  Review at: {pr_url}")
    print(f"  PASS")


if __name__ == "__main__":
    run()
