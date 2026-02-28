# nexus3-maven-versions-retrieve-with-iq-firewall

Fetch top-N Maven dependency versions from Nexus3 with SonatypeIQ
`firewall.status=allow` (non-quarantined), using copilot scripts instead
of the sonatype-nexus3 MCP tool.

## Why a skill instead of an MCP tool?

Nexus3 open-source / personal deployments **lack the SonatypeIQ license**:
- Nexus3 UI shows no firewall status column
- Nexus3 REST API (`/service/rest/v1/search`) does not return `firewall.status`
- The `sonatype-nexus3` MCP tool has no firewall data to report

This skill wraps existing copilot scripts that directly call:
- `curl` → Nexus3 REST API for component version listing
- `gcloud` CLI → Google Artifact Registry for firewall status (if available)
- Nexus3 IQ API (`/service/rest/v1/firewall/component`) for licensed instances

## Directory layout

```
nexus3-maven-versions-retrieve-with-iq-firewall/
├── skill.yaml         # AgentFlowSpec (loaded by flowgent skill loader)
├── SKILL.md           # This file — docs + agent instructions
├── scripts/
│   ├── query-nexus3.sh        # Standalone test script
│   └── filter-versions.py     # Version filtering + dedup logic
├── references/
│   └── nexus3-api.md          # Nexus3 REST API reference
└── workspace/                 # Runtime scratch space
```

## Inputs

| Field | Type | Description |
|-------|------|-------------|
| `maven_coordinates` | string | GAV coordinates from SonatypeIQ scan (`groupId:artifactId:version`) |
| `top_n` | integer | Number of latest safe versions to return (default: 3) |
| `repo` | string | Target repository name for context |

## Output

```json
{
  "versions": [
    {"version": "2.3.0", "firewall_status": "allow", "source": "nexus3"},
    {"version": "2.2.1", "firewall_status": "unknown", "source": "nexus3"},
    {"version": "2.1.0", "firewall_status": "blocked", "source": "nexus3"}
  ]
}
```

`firewall_status=unknown` is treated as allowed (environment lacks IQ license).

## Agent instructions (for the filter-results node)

You are a dependency security analyst. Given a raw JSON array of Maven
dependency versions with firewall status, your task is:

1. Filter OUT versions where `firewall_status == "blocked"` (quarantined).
2. Keep versions where `firewall_status` is `"allow"` or `"unknown"`.
   Treat `"unknown"` as allowed — the environment may lack IQ license.
3. Sort remaining versions by version number descending (semantic versioning).
4. Return at most `top_n` versions. If fewer remain after filtering, return all.
5. Output STRICT JSON with key `"versions"`.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `NEXUS3_URL` | `http://nexus3:8081` | Nexus3 base URL |
| `NEXUS3_USER` | `admin` | Nexus3 username |
| `NEXUS3_PASSWORD` | `admin` | Nexus3 password |
| `NEXUS3_REPO` | `maven-releases` | Nexus3 repository name |
