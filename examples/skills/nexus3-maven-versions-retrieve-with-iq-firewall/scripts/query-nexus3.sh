#!/bin/bash
# Standalone test script for the Nexus3 dependency firewall check.
# Usage:
#   NEXUS3_URL=http://nexus3:8081 INPUT_MAVEN_COORDINATES="com.example:artifact:1.0.0" \
#     ./query-nexus3.sh
#
# This mirrors the sandbox node script in skill.yaml. Use for local
# development and testing outside the flowgent runtime.

set -euo pipefail

N3URL="${NEXUS3_URL:-http://nexus3:8081}"
N3USER="${NEXUS3_USER:-admin}"
N3PASS="${NEXUS3_PASSWORD:-admin}"
COORDS="${INPUT_MAVEN_COORDINATES:-}"
TOPN="${INPUT_TOP_N:-3}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ -z "$COORDS" ]; then
  echo '{"error":"INPUT_MAVEN_COORDINATES is required (format: groupId:artifactId:version)"}'
  exit 1
fi

IFS=':' read -r GID AID CURVER <<< "$COORDS"

echo ">> Querying Nexus3: $N3URL, group=$GID, artifact=$AID, current=$CURVER" >&2

RESULTS=$(curl -s --noproxy '*' -u "${N3USER}:${N3PASS}" \
  "$N3URL/service/rest/v1/search?group=$GID&name=$AID&sort=version&direction=desc" \
  2>/dev/null || echo '{"items":[]}')

echo ">> Filtering versions..." >&2
VERSIONS=$(TOPN="$TOPN" CURVER="$CURVER" python3 "$SCRIPT_DIR/filter-versions.py" <<< "$RESULTS" 2>/dev/null || echo '[]')
echo ">> Top-N versions: $VERSIONS" >&2

# Check firewall status for each candidate
printf '["'
FIRST=1
for VER in $(echo "$VERSIONS" | python3 -c "import sys,json; [print(v) for v in json.load(sys.stdin)]" 2>/dev/null); do
  FW="unknown"

  # Try gcloud artifact registry
  if command -v gcloud &>/dev/null; then
    FW=$(gcloud artifacts packages list \
      --repository="${NEXUS3_REPO:-maven-releases}" --location=us-central1 \
      --package="$AID" --format="value(firewallStatus)" 2>/dev/null || echo "unknown")
  fi

  # Try Nexus3 IQ firewall API (works only with SonatypeIQ license)
  if [ "$FW" = "unknown" ]; then
    FW=$(curl -s --noproxy '*' -u "${N3USER}:${N3PASS}" \
      "$N3URL/service/rest/v1/firewall/component?group=$GID&name=$AID&version=$VER" \
      2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('firewallStatus','unknown'))" 2>/dev/null || echo "unknown")
  fi

  [ "$FIRST" = 1 ] && FIRST=0 || printf '","'
  printf '{"version":"%s","firewall_status":"%s"}' "$VER" "$FW"
done
printf '"]\n'
