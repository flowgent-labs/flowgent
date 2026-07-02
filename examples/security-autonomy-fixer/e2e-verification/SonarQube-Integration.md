# SonarQube Deployment & CI Integration

**Date:** 2026-05-29

---

## 1. Deploy SonarQube

```bash
cd deploy/docker/sonarqube
docker compose up -d
```

This starts:
- `sonarqube-postgres` — PostgreSQL 18.3 (health check: pg_isready)
- `sonarqube` — SonarQube 26.4.0 Community (health check: API status endpoint)
- `sonarqube-init` — One-shot container that resets admin password on first boot

### Auto Password Reset

The `sonarqube-init` service waits for SonarQube to be healthy, then calls:

```http
POST /api/users/change_password?login=admin&password=Abcd1234@sonar&previousPassword=admin
```

This ensures the admin password is always `Abcd1234@sonar` after deployment, even if
the SonarQube data volume is recreated.

### Credentials

| Item | Value |
|------|-------|
| URL | `http://localhost:9000` (or `https://sonarqube.wl4g.com` if behind Cloudflare) |
| Admin user | `admin` |
| Admin password | `Abcd1234@sonar` |

---

## 2. Generate API Token

After deployment, generate a token for CI use:

1. Log in to SonarQube at `https://sonarqube.wl4g.com`
2. Go to **My Account** → **Security** → **Generate Token**
3. Name: `flowgent-ci`
4. Copy the token — it's shown only once.

Save as GitHub secret (requires valid GH_TOKEN with repo admin scope):
```bash
gh secret set SONAR_TOKEN --body "squ_413955935468a7aacc7977053eda43d585e4da04" -R wl4g/rengine
gh secret set SONAR_HOST_URL --body "https://sonarqube.wl4g.com" -R wl4g/rengine
```

---

## 3. Rengine CI Workflow — GitHub Actions

Two workflows are configured for the rengine repository:

### 3.1 PR Workflow — Incremental Scan

Triggered on every `pull_request` to any branch. Runs tests with JaCoCo coverage,
then SonarQube scan with PR-specific parameters for incremental coverage:

```yaml
name: "PR: Test + SonarQube Incremental Scan"

on:
  pull_request:
    branches: ['**']

jobs:
  test-and-sonar:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-java@v3
        with: { distribution: temurin, java-version: 11 }

      # Run tests with coverage
      - run: ./tools/build/run.sh build-maven -DskipTests=false

      # SonarQube incremental scan (PR analysis)
      - run: |
          ./tools/build/run.sh sonar \
            -Dsonar.host.url=${{ secrets.SONAR_HOST_URL }} \
            -Dsonar.token=${{ secrets.SONAR_TOKEN }} \
            -Dsonar.pullrequest.key=${{ github.event.pull_request.number }} \
            -Dsonar.pullrequest.branch=${{ github.head_ref }} \
            -Dsonar.pullrequest.base=${{ github.base_ref }}
```

### 3.2 Push to Main — Full Scan

Triggered on push to `main`/`master`. Runs full coverage scan (no PR params):

```yaml
name: "Push Main: Test + SonarQube Full Scan"

on:
  push:
    branches: [main, master]

jobs:
  test-and-sonar:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-java@v3
        with: { distribution: temurin, java-version: 11 }

      - run: ./tools/build/run.sh build-maven -DskipTests=false

      # Full scan — no PR parameters
      - run: |
          ./tools/build/run.sh sonar \
            -Dsonar.host.url=${{ secrets.SONAR_HOST_URL }} \
            -Dsonar.token=${{ secrets.SONAR_TOKEN }}
```

### Key Parameters

| Parameter | PR (Incremental) | Main (Full) |
|-----------|-----------------|--------------|
| `sonar.pullrequest.key` | PR number | (not set) |
| `sonar.pullrequest.branch` | Source branch | (not set) |
| `sonar.pullrequest.base` | Target branch | (not set) |
| JaCoCo report | `**/target/site/jacoco/jacoco.xml` | Same |

When `pullrequest.key` is set, SonarQube performs incremental analysis — only
changed lines are counted for coverage and quality gate. Without it, the full
project is analyzed.

### SonarQube Project Configuration

The `sonar-project.properties` for rengine:

```properties
sonar.projectKey=rengine
sonar.projectName=Rengine
sonar.sources=common/src/main/java,apiserver/src/main/java,controller/src/main/java,service/src/main/java
sonar.java.binaries=common/target/classes,apiserver/target/classes,controller/target/classes,service/target/classes
sonar.tests=common/src/test/java,apiserver/src/test/java,controller/src/test/java,service/src/test/java
sonar.coverage.jacoco.xmlReportPaths=**/target/site/jacoco/jacoco.xml
sonar.java.source=11
```

---

## 4. GitHub Webhook (NOT REQUIRED)

A GitHub webhook to Flowgent is **not needed** for the current setup. Here's why:

| Role | Handled By |
|------|-----------|
| CI: test + JaCoCo + SonarQube scan | **GitHub Actions** (`sonarqube_pr_scan.yaml`) |
| Trigger Flowgent security fixer flow | **Not applicable** — Flowgent runs inside K3s without a public endpoint |

GitHub Actions is the standard CI mechanism. A webhook would only be necessary
if Flowgent had a publicly accessible endpoint to receive GitHub events directly
(like a self-hosted Jenkins or Tekton server would). Since Flowgent is deployed
inside a K3s cluster behind NAT/Cloudflare, the Actions workflow is the correct
approach.

If a public Flowgent endpoint becomes available in the future, the webhook URL
would be `https://flowgent.wl4g.com/_/webhooks/github`.

---

## 5. Verification Checklist

| Step | Command | Expected |
|------|---------|----------|
| SonarQube up | `curl -s -u admin:Abcd1234@sonar https://sonarqube.wl4g.com/api/system/status` | `{"status":"UP"}` |
| Generate token | SonarQube UI → My Account → Security | Returns `squ_xxx...` |
| GitHub secret | `gh secret list -R wl4g/rengine` | Shows `SONAR_TOKEN`, `SONAR_HOST_URL` |
| PR scan | Create PR → GitHub Actions | Build + JaCoCo + SonarQube analysis |
| Full scan | Merge to main → GitHub Actions | Build + full SonarQube analysis |
| Webhook | Not required (GitHub Actions handles CI) | N/A |

---

## 6. GH_TOKEN Troubleshooting

When `gh` CLI returns `HTTP 401` even though the token is valid, the root cause
is usually that `~/.bashrc` is **not sourced in non-interactive shells**.

Most `.bashrc` files have a guard at the top:

```bash
[ -z "$PS1" ] && return   # exit if not interactive
```

This means `bash -c 'source ~/.bashrc && gh ...'` silently skips the entire file,
including `source ~/.wl4gshrc.sec` which sets `GH_TOKEN`.

**Fix:** Use `bash -i -c '...'` (interactive mode) or extract the token directly:

```bash
# Method 1: Interactive bash (simplest)
bash -i -c 'gh secret set FOO --body bar -R owner/repo'

# Method 2: Extract token directly
export GH_TOKEN=$(bash -i -c 'echo $GH_TOKEN' 2>/dev/null)
gh secret set FOO --body bar -R owner/repo
```

---

## 7. Unified Setup Script

Save as `/tmp/sonarqube-integration-setup.sh` and run with `bash -i /tmp/sonarqube-integration-setup.sh`.

The script is **idempotent** — secrets are updated, webhooks are created only
if a matching URL doesn't already exist.

```bash
#!/bin/bash
# ═══════════════════════════════════════════════════════════════
# /tmp/sonarqube-integration-setup.sh — Configure GitHub repo for SonarQube CI
#
# Usage:
#   bash -i /tmp/sonarqube-integration-setup.sh <owner/repo>
#
# Prerequisites:
#   - GH_TOKEN set in ~/.bashrc or ~/.wl4gshrc.sec (needs repo admin + workflow scopes)
#   - SonarQube admin password and API token (see §1-§2)
# ═══════════════════════════════════════════════════════════════
set -euo pipefail

REPO="${1:-wl4g/rengine}"
SONAR_HOST="${SONAR_HOST_URL:-https://sonarqube.wl4g.com}"
SONAR_TOKEN="${SONAR_TOKEN:-squ_413955935468a7aacc7977053eda43d585e4da04}"
FLOWGENT_WEBHOOK_URL="${FLOWGENT_WEBHOOK_URL:-https://flowgent.wl4g.com/_/webhooks/github}"

# ── Detect GH_TOKEN ───────────────────────────────────────────
if [ -z "${GH_TOKEN:-}" ]; then
  # Try extracting from interactive bash (handles .bashrc guard)
  GH_TOKEN=$(bash -i -c 'echo $GH_TOKEN' 2>/dev/null || true)
fi
if [ -z "${GH_TOKEN:-}" ] || [ "${#GH_TOKEN}" -lt 10 ]; then
  echo "ERROR: GH_TOKEN not found. Source ~/.bashrc first or set GH_TOKEN env var."
  echo "  Method 1: bash -i -c '$(basename "$0") $REPO'"
  echo "  Method 2: export GH_TOKEN=github_pat_..."
  exit 1
fi

export GH_TOKEN
echo ">> GH_TOKEN: ${GH_TOKEN:0:20}... (len=${#GH_TOKEN})"

# ── Validate gh CLI ────────────────────────────────────────────
if ! gh auth status &>/dev/null; then
  echo "$GH_TOKEN" | gh auth login --with-token
fi

echo ">> Authenticated as: $(gh api user --jq '.login')"

# ── Set SonarQube Secrets ──────────────────────────────────────
echo ">> Setting SONAR_HOST_URL ..."
gh secret set SONAR_HOST_URL --body "$SONAR_HOST" -R "$REPO"

echo ">> Setting SONAR_TOKEN ..."
gh secret set SONAR_TOKEN --body "$SONAR_TOKEN" -R "$REPO"

echo ">> Secrets:"
gh secret list -R "$REPO" | grep SONAR || echo "  (SONAR_* secrets set)"

# ── Create Webhook (idempotent) ────────────────────────────────
EXISTING=$(gh api "repos/$REPO/hooks" --jq '.[].config.url' 2>/dev/null || echo "")
if echo "$EXISTING" | grep -qF "$FLOWGENT_WEBHOOK_URL"; then
  echo ">> Webhook already exists: $FLOWGENT_WEBHOOK_URL"
else
  echo ">> Creating webhook: $FLOWGENT_WEBHOOK_URL"
  gh api "repos/$REPO/hooks" \
    -f name=web \
    -F active=true \
    -f "config[url]=$FLOWGENT_WEBHOOK_URL" \
    -f "config[content_type]=json" \
    -f "events[]=pull_request" \
    -f "events[]=push"
  echo ">> Webhook created."
fi

# ── Verify ─────────────────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Setup Complete"
echo "═══════════════════════════════════════════════════════════"
echo "  Repo:     $REPO"
echo "  SonarQube: $SONAR_HOST"
echo "  Webhook:  $FLOWGENT_WEBHOOK_URL"
echo ""
echo "  Verify:"
echo "    gh secret list -R $REPO | grep SONAR"
echo "    gh api repos/$REPO/hooks --jq '.[].config.url'"
echo "    curl -s -u admin:Abcd1234@sonar $SONAR_HOST/api/system/status"
echo "═══════════════════════════════════════════════════════════"
```
