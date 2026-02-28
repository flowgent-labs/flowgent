#!/bin/bash
# ═══════════════════════════════════════════════════════════════
# setup_sonarqube_ci.sh — Configure GitHub repo for SonarQube CI
#
# Usage:
#   bash -i docs/20-L2-Integration-SonarQube-setup.sh [owner/repo]
#
# IMPORTANT: Must use `bash -i` (interactive) because ~/.bashrc
# typically has a `[ -z "$PS1" ] && return` guard that skips
# sourcing in non-interactive shells, which means ~/.wl4gshrc.sec
# never runs and GH_TOKEN is never set.
#
# Prerequisites:
#   - GH_TOKEN in ~/.bashrc or ~/.wl4gshrc.sec
#     (needs repo, admin:repo_hook, workflow scopes)
#   - SonarQube admin password and API token (see doc)
# ═══════════════════════════════════════════════════════════════
set -euo pipefail

REPO="${1:-wl4g/rengine}"
SONAR_HOST="${SONAR_HOST_URL:-https://sonarqube.wl4g.com}"
SONAR_TOKEN="${SONAR_TOKEN:-squ_413955935468a7aacc7977053eda43d585e4da04}"

# ── Detect GH_TOKEN ───────────────────────────────────────────
if [ -z "${GH_TOKEN:-}" ]; then
  GH_TOKEN=$(bash -i -c 'echo $GH_TOKEN' 2>/dev/null || true)
fi
if [ -z "${GH_TOKEN:-}" ] || [ "${#GH_TOKEN}" -lt 10 ]; then
  echo "ERROR: GH_TOKEN not found."
  echo "  Run with:  bash -i $0 $REPO"
  echo "  Or:        export GH_TOKEN=github_pat_..."
  exit 1
fi
export GH_TOKEN

echo ">> Token: ${GH_TOKEN:0:20}... (len=${#GH_TOKEN})"

# ── Auth ───────────────────────────────────────────────────────
if ! gh auth status &>/dev/null; then
  echo "$GH_TOKEN" | gh auth login --with-token
fi
echo ">> User: $(gh api user --jq '.login')"

# ── Secrets ────────────────────────────────────────────────────
echo ">> Setting SONAR_HOST_URL ..."
gh secret set SONAR_HOST_URL --body "$SONAR_HOST" -R "$REPO"

echo ">> Setting SONAR_TOKEN ..."
gh secret set SONAR_TOKEN --body "$SONAR_TOKEN" -R "$REPO"

echo ">> Secrets set:"
gh secret list -R "$REPO" | grep SONAR || true

# ── Done ───────────────────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Setup Complete — $REPO"
echo "═══════════════════════════════════════════════════════════"
echo "  SonarQube: $SONAR_HOST"
echo ""
echo "  Verify secrets:  gh secret list -R $REPO | grep SONAR"
echo "  Verify sonar:    curl -s -u admin:Abcd1234@sonar $SONAR_HOST/api/system/status"
echo "═══════════════════════════════════════════════════════════"
