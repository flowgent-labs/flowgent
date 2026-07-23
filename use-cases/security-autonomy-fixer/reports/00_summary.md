# E2E Verification Summary

**Result**: 0/1 passed  
**Duration**: 3.2s  
**Timestamp**: 2026-07-21 23:37:40  

| # | Scenario | Status | Duration |
|---|----------|--------|----------|
| 10 | PR Commits | FAIL | 3.2s |

## Failures

### Scenario 10: PR Commits — Verify Fix Commits on Target PR
```
Critical PR checks failed: ['Flowgent security-fix commits on PR']. Flowgent security-autonomy-fixer has NOT pushed fix commits to https://github.com/wl4g/rengine/pull/4. Check: (1) GitHub MCP token has repo:write scope, (2) commit-fixes and create-pr nodes completed successfully, (3) MCP tool names in flow definition match the real GitHub MCP server tools.
Traceback (most recent call last):
  File "/home/agent/flowgent/./use-cases/security-autonomy-fixer/e2e-verification/runner.py", line 179, in run_scenario
    mod.run()
  File "/home/agent/flowgent/use-cases/security-autonomy-fixer/e2e-verification/scenarios/10_pr_commit_verifier.py", line 214, in run
    raise AssertionError(
AssertionError: Critical PR checks failed: ['Flowgent security-fix commits on PR']. Flowgent security-autonomy-fixer has NOT pushed fix commits to https://github.com/wl4g/rengine/pull/4. Check: (1) GitHub MCP token has repo:write scope, (2) commit-fixes and create-pr nodes completed successfully, (3) MCP tool names in flow definition match the real GitHub MCP server tools.

```

