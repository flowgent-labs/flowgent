# Scenario 01: Infrastructure — Pre-Deployment & Pod Readiness

**Status**: PASS  
**Duration**: 3.7s  
**Timestamp**: 2026-07-14 19:02:36  

## Output
```

── L1: Pre-Deployment ──
  [1.1] K3s nodes OK (Ready found)
  [1.2] kube-system pods: 2 Running of 5 total
  [1.3] SonarQube: http://localhost:9000 → HTTP 403 (non-critical)
  [1.5] EMQX reachable: localhost:1883
  [1.7] Docker image flowgent-core found

── L2: Helm State ──
  [2.5] Helm release: flowgent status=deployed chart=flowgent-0.1.0 app_version=latest
  [2.6] K8s resources: 18 items (deploy/svc/configmap)

── L3: Pod Readiness ──
  [3.1] Flowgent pods: 32 total
    flowgent-a2a-76cbb4b464-5h58l                      phase=Failed     ready=0/0
    flowgent-a2a-76cbb4b464-6m5mk                      phase=Succeeded  ready=0/1
    flowgent-a2a-76cbb4b464-6t647                      phase=Failed     ready=0/0
    flowgent-a2a-76cbb4b464-r6s8n                      phase=Succeeded  ready=0/1
    flowgent-a2a-76cbb4b464-xtxg5                      phase=Failed     ready=0/0
    flowgent-a2a-76cbb4b464-z45t5                      phase=Failed     ready=0/0
    flowgent-a2a-7f4bc6b56b-9lvcq                      phase=Running    ready=1/1
    flowgent-a2a-7f4bc6b56b-f4bgc                      phase=Running    ready=1/1
    flowgent-apiserver-5c5cf47f4b-hcjlf                phase=Running    ready=1/1
    flowgent-apiserver-5c5cf47f4b-qp5nf                phase=Running    ready=1/1
    flowgent-apiserver-6f8b77f59f-hfqtj                phase=Failed     ready=0/0
    flowgent-apiserver-6f8b77f59f-lppgz                phase=Failed     ready=0/0
    flowgent-apiserver-7465946499-2xxrr                phase=Failed     ready=0/0
    flowgent-apiserver-7465946499-5bsrh                phase=Failed     ready=0/0
    flowgent-apiserver-7465946499-k5bt2                phase=Failed     ready=0/1
    flowgent-apiserver-7465946499-ndwfr                phase=Failed     ready=0/1
    flowgent-apiserver-7465946499-xbpx7                phase=Failed     ready=0/0
    flowgent-apiserver-7465946499-zw92n                phase=Failed     ready=0/0
    flowgent-controller-556ddf766b-t99jl               phase=Failed     ready=0/0
    flowgent-controller-556ddf766b-t9xgv               phase=Failed     ready=0/0
    flowgent-controller-5d9f6c6bd5-48dck               phase=Running    ready=1/1
    flowgent-controller-7cbd9f5894-hr8bf               phase=Failed     ready=0/0
    flowgent-controller-7cbd9f5894-z6x47               phase=Failed     ready=0/0
    flowgent-controller-7cbd9f5894-z896w               phase=Succeeded  ready=0/1
    flowgent-notifier-6d9c745c8-89l9b                  phase=Running    ready=1/1
    flowgent-notifier-6d9c745c8-dw4d6                  phase=Running    ready=1/1
    flowgent-notifier-6f6b459df-cfms2                  phase=Failed     ready=0/0
    flowgent-notifier-6f6b459df-ldvpb                  phase=Failed     ready=0/0
    flowgent-notifier-6f6b459df-qmq9g                  phase=Succeeded  ready=0/1
    flowgent-notifier-6f6b459df-rf748                  phase=Succeeded  ready=0/1
    flowgent-notifier-6f6b459df-shfvp                  phase=Failed     ready=0/0
    flowgent-notifier-6f6b459df-w5dk2                  phase=Failed     ready=0/0
  [3.8] No CrashLoopBackOff containers
  [3.1] WARN: 7/32 Running. Not running: ['flowgent-a2a-76cbb4b464-5h58l', 'flowgent-a2a-76cbb4b464-6m5mk', 'flowgent-a2a-76cbb4b464-6t647', 'flowgent-a2a-76cbb4b464-r6s8n', 'flowgent-a2a-76cbb4b464-xtxg5', 'flowgent-a2a-76cbb4b464-z45t5', 'flowgent-apiserver-6f8b77f59f-hfqtj', 'flowgent-apiserver-6f8b77f59f-lppgz', 'flowgent-apiserver-7465946499-2xxrr', 'flowgent-apiserver-7465946499-5bsrh', 'flowgent-apiserver-7465946499-k5bt2', 'flowgent-apiserver-7465946499-ndwfr', 'flowgent-apiserver-7465946499-xbpx7', 'flowgent-apiserver-7465946499-zw92n', 'flowgent-controller-556ddf766b-t99jl', 'flowgent-controller-556ddf766b-t9xgv', 'flowgent-controller-7cbd9f5894-hr8bf', 'flowgent-controller-7cbd9f5894-z6x47', 'flowgent-controller-7cbd9f5894-z896w', 'flowgent-notifier-6f6b459df-cfms2', 'flowgent-notifier-6f6b459df-ldvpb', 'flowgent-notifier-6f6b459df-qmq9g', 'flowgent-notifier-6f6b459df-rf748', 'flowgent-notifier-6f6b459df-shfvp', 'flowgent-notifier-6f6b459df-w5dk2']
  [3.3] Apiserver healthz: HTTP 200 — {"status":"ok"}

  [3.9] apiserver logs: no matching pods (may not be enabled) — SKIP
  [3.9] controller logs: no matching pods (may not be enabled) — SKIP
  [3.9] notifier logs (last 20 lines): 0 error/fatal/panic — OK
  [3.10] 1 dedicated per-flow JM pod(s) found (leftover from a previous run? see Environment Reset in VERIFICATION.md)

  Preflight check complete — verify items flagged WARN above.

```
