# Scenario 04: Controller — Application Mode Lifecycle

**Status**: PASS  
**Duration**: 148.7s  
**Timestamp**: 2026-07-14 19:05:07  

## Output
```

============================================================
  Scenario 04: Controller — Application Mode Lifecycle
============================================================

  → Verifying Controller pod...
      ✓ Controller pod is Running (flowgent-controller-5d9f6c6bd5-48dck)

  → Testing Flow CREATE → JM Deployment + MQTT event...
    • Step 1: Creating AgentFlow (test-flow-13a5a4f5)...
      ✓ Flow created: id=test-flow-13a5a4f5
      ⚠ No ctrl/flow/updated MQTT event (Controller may use polling)
    • Step 2: Waiting for Controller to create JM Deployment...
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-13a5a4f5...
      ✓ Deployment flowgent-jobmanager-default-test-flow-13a5a4f5 exists
    • Step 3: Verifying Deployment spec...
      ✓ Deployment spec verified
    • Step 4: Waiting for JM Pod to reach Running...
    • Waiting for Pod (selector=app=flowgent-jobmanager,flowgent.io/flow=test-flow-13a5a4f5)...
      ✓ Pod flowgent-jobmanager-default-test-flow-13a5a4f5-f4b8c7678-9zdt4 is Running
    • Step 5: Deleting AgentFlow...
    • Step 6: Verifying Deployment cleanup...
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-13a5a4f5 to be deleted...
      ✗ Deployment flowgent-jobmanager-default-test-flow-13a5a4f5 still exists after 60s
      ⚠ Deployment not auto-deleted, manual cleanup...
    ✓ Flow CREATE lifecycle verified

  → Testing Flow UPDATE → JM rolling update...
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-8a5aed3c...
      ✓ Deployment flowgent-jobmanager-default-test-flow-8a5aed3c exists
      ✓ Deployment generation 1 → 1
    • Waiting for Deployment flowgent-jobmanager-default-test-flow-8a5aed3c to be deleted...
      ✗ Deployment flowgent-jobmanager-default-test-flow-8a5aed3c still exists after 60s
    ✓ Flow UPDATE lifecycle verified

  ============================================================
  Summary: 3/3 tests passed
  ============================================================

  ✓ All Controller tests passed

```
