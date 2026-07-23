# E2E Verification Summary

**Result**: 0/1 passed  
**Duration**: 0.2s  
**Timestamp**: 2026-07-21 09:09:17  

| # | Scenario | Status | Duration |
|---|----------|--------|----------|
| 11 | Knowledge | FAIL | 0.2s |

## Failures

### Scenario 11: Knowledge — RAG Retrieval & Injection
```
Create knowledge failed: 404 404 page not found

Traceback (most recent call last):
  File "/home/agent/flowgent/examples/security-autonomy-fixer/e2e-verification/runner.py", line 179, in run_scenario
    mod.run()
  File "/home/agent/flowgent/examples/security-autonomy-fixer/e2e-verification/scenarios/11_knowledge_verifier.py", line 131, in run
    kid = seed_knowledge()
          ^^^^^^^^^^^^^^^^
  File "/home/agent/flowgent/examples/security-autonomy-fixer/e2e-verification/scenarios/11_knowledge_verifier.py", line 48, in seed_knowledge
    assert resp.status_code in (200, 201), f"Create knowledge failed: {resp.status_code} {resp.text}"
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
AssertionError: Create knowledge failed: 404 404 page not found


```

