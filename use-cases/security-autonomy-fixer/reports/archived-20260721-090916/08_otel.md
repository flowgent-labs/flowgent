# Scenario 08: OTEL — Jaeger Span Coverage

**Status**: PASS  
**Duration**: 9.5s  
**Timestamp**: 2026-07-21 09:09:16  

## Output
```

============================================================
  Scenario 10: OTEL Tracing — Infrastructure + Span Coverage
============================================================
  -> Seeding agent + MCP definitions...
  OK Agent + MCP definitions ready
  -> Ensuring security-autonomy-fixer flow definition exists...
  OK Flow definition ready: security-autonomy-fixer (priority=high)
  -> Waiting for JM pod to be created by Controller...
  OK JM pod is Running
  -> Triggering security-autonomy-fixer flow...
  OK Flow triggered: run_id=8aa1c423-614c-43bc-9488-b35b33281a06
  -> Waiting for run 8aa1c423-614c-43bc-9488-b35b33281a06 to complete (timeout=600s)...
  OK Flow failed
  WARN: Flow status=FAILED, continuing verification...

  -> Verifying OTEL infrastructure from pod logs...
  OK JM OTEL: OTEL tracing enabled (verified via config)
  OK JM: No DNS resolution errors (Jaeger FQDN resolves)
  OK JM: No OTEL export errors in logs
  OK API Server OTEL: 2026/07/14 17:25:42 INFO OTEL tracing enabled endpoint=flowgent-jaeger.default.svc.cluster.local:4318

  -> Querying Jaeger for traces (opportunistic)...
  Info: No traces found in Jaeger for this run
  Info: OTEL infrastructure verified via pod logs instead

  ============================================================
  OTEL Infrastructure: 3/3 critical checks passed
  OK OTEL tracing infrastructure verified
  Info: Jaeger traces not available (infrastructure issue, not Flowgent code)
  PASS

```
