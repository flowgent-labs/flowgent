---
name: nexus3-retrieval
description: Query Nexus3 REST API and SonatypeIQ Web UI API to fetch top-N non-quarantined Maven dependency versions with firewall status. Use when analyzing dependency security, finding safe library versions, checking SonatypeIQ firewall compliance, upgrading Maven dependencies, or resolving vulnerable transitive dependencies.
compatibility: Requires curl, python3, and network access to nexus3:8081 and sonatypeiq:8070
metadata:
  version: "1.0"
---

# nexus3-retrieval

A Flowgent skill wrapping Nexus3 + SonatypeIQ APIs as a toolset for dependency firewall checking.

This SKILL.md serves as a **tool catalog** — AI agents/LLMs read it to decide which script to invoke. The skill is a DAG node (`type: skill`) in a parent flow.

## Available Tools

| Script | Subcommand | What It Does |
|--------|-----------|-------------|
| `nexus3-iq.sh` | `get-available-versions <gid> <aid> [ver]` | Queries Nexus3 REST API + SonatypeIQ Web UI API, returns top-N non-quarantined Maven dependency versions |

## Agent Usage Pattern

```
User: "find safe versions of com.wl4g:rengine-service"
→ Agent: reads SKILL.md tool catalog
→ Selects: nexus3-iq.sh get-available-versions com.wl4g rengine-service
→ Returns: JSON with top-N versions (excludes quarantined/snapshots/current)
```

## Credentials

Injected via environment variables (K8s Secrets → Sandbox Pod Env):

| Env | Default | From |
|-----|---------|------|
| `NEXUS3_URL` | `http://nexus3:8081` | Helm values |
| `NEXUS3_USER` | `admin` | K8s secret `nexus3-creds` |
| `NEXUS3_PASSWORD` | — | K8s secret `nexus3-creds` |
| `SONATYPEIQ_URL` | `http://sonatypeiq:8070` | Helm values |
| `SONATYPEIQ_USER` | — | K8s secret `iq-creds` (optional) |
| `SONATYPEIQ_PASS` | — | K8s secret `iq-creds` (optional) |

## Workspace

Sandbox scripts execute in a shared workspace volume:

```
{workspace}/{namespaceId}/{flowId}/{runId}/{taskId}/
  ├── script.sh        ← written by SandboxExecutor
  ├── result.json      ← written by SandboxRunner after execution
  └── status
```

| Mode | Workspace Backend |
|------|------------------|
| Session | PVC (ReadWriteMany), shared across all sandbox pods |
| Application | PVC — dedicated per `{namespace}/{flow_id}`, provisioned by Controller |

This skill is **read-only** — it queries external APIs and returns data without modifying project files. Side effects (pom.xml edits, commits) are handled by subsequent DAG nodes in the parent flow.

## Output

```json
{
  "versions": [
    {"version":"2.3.0","firewall":"allow","recommended":true,"source":"iq"},
    {"version":"2.2.1","firewall":"allow","recommended":false,"source":"nexus3"}
  ],
  "warning":"",
  "total":2
}
```
