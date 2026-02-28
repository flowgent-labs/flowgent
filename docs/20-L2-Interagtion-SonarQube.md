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

```
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

## 4. GitHub Webhook → Flowgent

For the Flowgent security fixer flow to be triggered on PRs, a GitHub webhook
must be configured:

### 4.1 Create Webhook

```bash
gh api repos/wl4g/rengine/hooks \
  -f name=web \
  -f config.url=https://flowgent.wl4g.com/api/v1/default/webhooks/github \
  -f config.content_type=json \
  -f config.secret=flowgent-webhook-secret \
  -f events[]=pull_request \
  -f events[]=push
```

### 4.2 Flowgent Webhook Receiver

The Flowgent API server listens on `/_/webhooks/github` for push and PR events.
When a PR is received, it creates an `agentflow_run` with the webhook payload
as trigger, kicking off the Security Autonomy Fixer flow.

The webhook payload includes:
- `pull_request.number` — used as PR identifier in the flow
- `pull_request.head.ref` — source branch for checkout
- `pull_request.base.ref` — target branch for merge target
- `repository.clone_url` — repo URL for checkout

### 4.3 Verify Webhook

```bash
# List webhooks
gh api repos/wl4g/rengine/hooks

# Check recent deliveries
gh api repos/wl4g/rengine/hooks/<hook_id>/deliveries
```

---

## 5. Verification Checklist

| Step | Command | Expected |
|------|---------|----------|
| SonarQube up | `curl -s -u admin:Abcd1234@sonar https://sonarqube.wl4g.com/api/system/status` | `{"status":"UP"}` |
| Generate token | SonarQube UI → My Account → Security | Returns `squ_xxx...` |
| GitHub secret | `gh secret list -R wl4g/rengine` | Shows `SONAR_TOKEN`, `SONAR_HOST_URL` |
| PR scan | Create PR → GitHub Actions | Build + JaCoCo + SonarQube analysis |
| Full scan | Merge to main → GitHub Actions | Build + full SonarQube analysis |
| Webhook | `gh api repos/wl4g/rengine/hooks` | Shows active webhook |
