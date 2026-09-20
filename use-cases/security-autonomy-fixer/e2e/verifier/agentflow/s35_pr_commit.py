#!/usr/bin/env python3
"""
Scenario 35 — PR Commit Verifier: Check Flowgent's Fix Commits on Target PR

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
from __future__ import annotations

import os

from common import config
from verifier.agentflow.support import SecurityAutonomyFixture as c

GITHUB_API = c.GITHUB_API
REPO = c.PR_REPO
PR_NUMBER = c.PR_NUMBER
BRANCH = "fix/flowgent_sec_auto_fix"

GITHUB_TOKEN = os.getenv("GITHUB_TOKEN") or os.getenv("GH_TOKEN") or ""
HEADERS = {
    "Accept": "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
}
if GITHUB_TOKEN:
    HEADERS["Authorization"] = f"Bearer {GITHUB_TOKEN}"

FLOWGENT_SECURITY_FIX_PATTERNS = [
    "[security fix]",
    "auto-generated security",
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

HISTORICAL_COMMIT_SAMPLE_SIZE = 3


class PullRequestCommitOperations:
    """Class-owned operations for s35 pr commit."""

    @staticmethod
    def _print_commits(label: str, commits: list[dict], *, historical: bool = False) -> None:
        """Print complete current-run evidence while bounding historical report noise."""
        print(f"  {label}: {len(commits)}")
        visible = commits
        if historical and len(commits) > HISTORICAL_COMMIT_SAMPLE_SIZE:
            visible = commits[-HISTORICAL_COMMIT_SAMPLE_SIZE:]
            omitted = len(commits) - len(visible)
            print(f"    ... {omitted} historical commit(s) omitted")
        for commit in visible:
            print(
                f"    {commit['sha']} {commit['message'][:80]} "
                f"(author: {commit['author']})"
            )

    @staticmethod
    def _gh_get(path: str) -> dict:
        try:
            return c.gh_get_json(path)
        except AssertionError as exc:
            print(f"  FAIL: {exc}")
            return None

    @staticmethod
    def _verify_scenario():
        print("\n" + "=" * 60)
        print("  Scenario 35: PR Commits — Verify Fix Commits on Target PR")
        print("=" * 60)

        # 1. Get PR metadata
        print(f"\n  -> Querying PR #{PR_NUMBER} on {REPO}...")
        pr = PullRequestCommitOperations._gh_get(f"/repos/{REPO}/pulls/{PR_NUMBER}")
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

        baseline = c.load_pr_baseline()
        baseline_shas = set(baseline.get("shas") or [])
        baseline_count = int(baseline.get("commit_count") or 0)
        baseline_head = (baseline.get("head_sha") or "")[:8] or "none"
        print(f"  Baseline from s31: commits={baseline_count} head={baseline_head}")

        # 2. Get commits on the PR (only those unique to the head branch vs base)
        print(f"\n  -> Fetching commits on PR #{PR_NUMBER}...")
        try:
            commits = c.fetch_pr_commits(REPO, PR_NUMBER)
        except AssertionError as exc:
            print(f"  FAIL: {exc}")
            raise AssertionError(f"Cannot fetch commits for branch '{BRANCH}' — GitHub API unreachable")

        print(f"  Total commits on PR: {len(commits)}")

        # 3. Classify commits: security fixes vs CI/workflow vs other
        flowgent_security_fixes = []
        new_flowgent_security_fixes = []
        flowgent_ci_commits = []
        other_commits = []
        new_commits = []

        for commit in commits:
            commit_data = commit.get("commit", {})
            msg_full = commit_data.get("message", "")
            msg = msg_full.lower()
            author = commit_data.get("author", {}).get("name", "")
            committer = commit_data.get("committer", {}).get("name", "")
            full_sha = commit.get("sha", "")
            sha = full_sha[:8]

            is_security_fix = any(pattern in msg for pattern in FLOWGENT_SECURITY_FIX_PATTERNS)
            is_ci = any(pattern in msg for pattern in FLOWGENT_CI_PATTERNS)
            is_flowgent_author = "flowgent" in author.lower() or "flowgent" in committer.lower()
            is_flowgent_message = "flowgent" in msg or "auto-generated security" in msg or "[security fix]" in msg

            entry = {
                "sha": sha,
                "full_sha": full_sha,
                "message": msg_full.split("\n")[0],
                "author": author,
                "committer": committer,
            }

            is_new = bool(full_sha) and full_sha not in baseline_shas
            if is_new:
                new_commits.append(entry)

            if is_security_fix and (is_flowgent_author or is_flowgent_message):
                flowgent_security_fixes.append(entry)
                if is_new:
                    new_flowgent_security_fixes.append(entry)
            elif is_ci and is_flowgent_author:
                flowgent_ci_commits.append(entry)
            else:
                other_commits.append(entry)

        PullRequestCommitOperations._print_commits(
            "Flowgent security-fix commits",
            flowgent_security_fixes,
            historical=True,
        )
        if flowgent_security_fixes:
            print(f"  ✓ These are commits produced by the security-autonomy-fixer pipeline")
        PullRequestCommitOperations._print_commits("New commits since s31 baseline", new_commits)
        PullRequestCommitOperations._print_commits(
            "New Flowgent security-fix commits this run",
            new_flowgent_security_fixes,
        )
        PullRequestCommitOperations._print_commits(
            "Flowgent CI/workflow commits (NOT security fixes)",
            flowgent_ci_commits,
            historical=True,
        )
        PullRequestCommitOperations._print_commits("Other commits", other_commits[:5])

        # 4. Check files in PR for test coverage
        print(f"\n  -> Checking PR files for test coverage changes...")
        files_data = PullRequestCommitOperations._gh_get(f"/repos/{REPO}/pulls/{PR_NUMBER}/files?per_page=100")
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

        has_new_commits = len(new_commits) > 0 and len(commits) > baseline_count
        checks.append(("New commits since Scenario 31 baseline", has_new_commits, True))

        has_new_security_fixes = len(new_flowgent_security_fixes) > 0
        checks.append(("New Flowgent security-fix commits in this run", has_new_security_fixes, True))

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
        if flowgent_security_fixes and not new_flowgent_security_fixes:
            print(f"\n  ⚠  PR has historical Flowgent security-fix commits, but this run produced none.")

        critical_failures = [label for label, ok, critical in checks if not ok and critical]
        if critical_failures:
            raise AssertionError(
                f"Critical PR checks failed: {critical_failures}. "
                f"Flowgent security-autonomy-fixer has NOT pushed fix commits to {pr_url}. "
                f"Check: (1) GitHub MCP token has repo:write scope, "
                f"(2) commit-fixes and create-pr nodes completed successfully, "
                f"(3) MCP tool names in flow definition match the real GitHub MCP server tools."
            )

        if has_new_security_fixes:
            print(f"\n  ✓ Flowgent security-autonomy-fixer produced {len(new_flowgent_security_fixes)} new commit(s)")
        if test_files:
            print(f"  ✓ Test coverage changes detected in {len(test_files)} file(s)")
        else:
            print(f"  ⚠  No test file changes — security fixes should include test coverage")
        print(f"  Review at: {pr_url}")
        print(f"  PASS")







from common.model import RunContext, VerificationResult
from verifier import BaseVerifier


class PullRequestCommitVerifier(BaseVerifier):
    scenario_id = "35"
    title = "PR Commits — Verify Fix Commits on Target PR"

    def run(self) -> VerificationResult:
        return self.execute(lambda: self.step("verify target PR commits and security-fix provenance", self._verify_commits))

    @staticmethod
    def _verify_commits() -> None:
        PullRequestCommitOperations._verify_scenario()

def verifier(context: RunContext) -> VerificationResult:
    """Run the pull-request commit scenario."""
    return PullRequestCommitVerifier(context).run()
