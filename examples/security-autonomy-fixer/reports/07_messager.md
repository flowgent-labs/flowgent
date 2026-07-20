# Scenario 07: Messager — MQTT Topics + Sandbox Chain

**Status**: PASS  
**Duration**: 5.6s  
**Timestamp**: 2026-07-14 19:07:07  

## Output
```

============================================================
  Scenario 07: Messager — MQTT Topics + Sandbox Chain
============================================================
  ✓ Connected to MQTT broker: localhost:1883

  → Testing MQTT Topic Pairs...

    • Testing exec/plans (JM → TM)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/exec/plans
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/exec/plans
      ✓ exec/plans (JM → TM) verified

    • Testing exec/results (TM → JM, state-only)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/exec/results
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/exec/results
      ✓ exec/results (TM → JM, state-only) verified

    • Testing notify/event (Publisher → Notifier)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/notify/event
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/notify/event
      ✓ notify/event (Publisher → Notifier) verified

    • Testing notify/result (Notifier → Publisher)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/notify/result
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/notify/result
      ✓ notify/result (Notifier → Publisher) verified

    • Testing sign/request (TM → Wallet)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/sign/request
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/sign/request
      ✓ sign/request (TM → Wallet) verified

    • Testing sign/response (Wallet → TM)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/sign/response
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/sign/response
      ✓ sign/response (Wallet → TM) verified

    • Testing heartbeat/{tmId} (TM → JM)...
      → Published to flowgent/v1/heartbeat/tm-6ef6da02
      ← Received on flowgent/v1/heartbeat/tm-6ef6da02
      ✓ heartbeat/{tmId} (TM → JM) verified

    • Testing ctrl/flow/updated (API → Controller)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/ctrl/flow/updated
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/ctrl/flow/updated
      ✓ ctrl/flow/updated (API → Controller) verified

    • Testing ctrl/flow/deleted (API → Controller)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/ctrl/flow/deleted
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/ctrl/flow/deleted
      ✓ ctrl/flow/deleted (API → Controller) verified

    • Testing ctrl/run/created (API → Controller)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/ctrl/run/created
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/ctrl/run/created
      ✓ ctrl/run/created (API → Controller) verified

    • Testing ctrl/run/status (API → Controller)...
      → Published to flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/ctrl/run/status
      ← Received on flowgent/v1/default/flows/test-flow-0f1a8e81/runs/run-a404e633/ctrl/run/status
      ✓ ctrl/run/status (API → Controller) verified

  → Testing Sandbox E2E Chain...
    • Step 1: Setting up subscriptions...
    • Step 2: JM → exec/plans...
    • Step 3: TM receives exec/plans...
      ✓ TM received plan
    • Step 4: TM → sandbox/trigger...
    • Step 5: Sandbox receives trigger...
      ✓ Sandbox received trigger
    • Step 6: Sandbox → sandbox/result...
    • Step 7: TM receives sandbox/result...
      ✓ TM received result: exit_code=0, stdout contains 'Hello'
    • Step 8: [TM → API Server REST] (not tested in this scenario)
    • Step 9: TM → exec/results (state only)...
    • Step 10: JM receives exec/results...
      ✓ JM received state: COMPLETED
    ✓ Complete Sandbox E2E chain verified

  ============================================================
  Summary: 12/12 tests passed
  ============================================================

  ✓ All Messager tests passed

```
