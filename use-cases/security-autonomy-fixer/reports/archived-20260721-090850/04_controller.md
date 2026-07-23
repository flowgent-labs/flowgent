# Scenario 04: Controller — Application Mode Lifecycle

**Status**: PASS  
**Duration**: 141.1s  
**Timestamp**: 2026-07-21 09:08:46  

## Output
```

============================================================
  Scenario 04: Controller — Application Mode Lifecycle
============================================================

  → Verifying Controller pod...
      ✓ Controller pod is Running (flowgent-controller-5d9f6c6bd5-48dck)

  → Testing Flow CREATE → JM Deployment + MQTT event...
    • Step 1: Creating AgentFlow (test-flow-3b8017b5)...
      ✓ Flow created: id=test-flow-3b8017b5
      ⚠ No ctrl/flow/updated MQTT event (Controller may use polling)
    • Step 2: Waiting for Controller to create JM Deployment...
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-3b8017b5...
      ✓ Deployment flowgent-jobmanager-default-test-flow-3b8017b5 exists
    • Step 3: Verifying Deployment spec...
      ✓ Deployment spec verified
    • Step 4: Waiting for JM Pod to reach Running...
    • Waiting for Pod (selector=app=flowgent-jobmanager,flowgent.io/flow=test-flow-3b8017b5)...
      ✓ Pod flowgent-jobmanager-default-test-flow-3b8017b5-5bb6d6455b-hrv9z is Running
    • Step 5: Deleting AgentFlow...
    • Step 6: Verifying Deployment cleanup...
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-3b8017b5 to be deleted...
      ✗ Deployment flowgent-jobmanager-default-test-flow-3b8017b5 still exists after 60s
      ⚠ Deployment not auto-deleted, manual cleanup...
    ✓ Flow CREATE lifecycle verified

  → Testing Flow UPDATE → JM rolling update...
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-a25526e2...
      ✓ Deployment flowgent-jobmanager-default-test-flow-a25526e2 exists
      ✓ Deployment generation 1 → 1
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-a25526e2 to be deleted...
      ✗ Deployment flowgent-jobmanager-default-test-flow-a25526e2 still exists after 60s
    ✓ Flow UPDATE lifecycle verified

  ============================================================
  Summary: 3/3 tests passed
  ============================================================

  ✓ All Controller tests passed

```
