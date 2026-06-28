#!/bin/bash
# Seed the flowgent DB with e2e test data via the apiserver API.
# Run from inside an apiserver pod.
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:9999}"
TENANT="${TENANT:-default}"

echo "=== Seeding E2E test data to $API_BASE/api/v1/$TENANT ==="

# ── 1. MCP Servers (mock) ─────────────────────────────────
echo "--- Creating MCP servers ---"
curl -s -X POST "$API_BASE/api/v1/$TENANT/mcp" -H 'Content-Type: application/json' -d '{
  "name": "github",
  "enabled": true,
  "type": "local",
  "command": ["/app/mcp-server.sh", "github"],
  "args": [],
  "env": {}
}' | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  github: {d.get(\"name\",\"?\")} id={d.get(\"id\",\"?\")}')"

curl -s -X POST "$API_BASE/api/v1/$TENANT/mcp" -H 'Content-Type: application/json' -d '{
  "name": "sonarqube",
  "enabled": true,
  "type": "local",
  "command": ["/app/mcp-server.sh", "sonarqube"],
  "args": [],
  "env": {}
}' | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  sonarqube: {d.get(\"name\",\"?\")} id={d.get(\"id\",\"?\")}')"

curl -s -X POST "$API_BASE/api/v1/$TENANT/mcp" -H 'Content-Type: application/json' -d '{
  "name": "sonatype-iq",
  "enabled": true,
  "type": "local",
  "command": ["/app/mcp-server.sh", "sonatype-iq"],
  "args": [],
  "env": {}
}' | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  sonatype-iq: {d.get(\"name\",\"?\")} id={d.get(\"id\",\"?\")}')"

# ── 2. LLM Provider ───────────────────────────────────────
DEEPSEEK_KEY="${DEEPSEEK_APIKEY:-sk-default}"
echo "--- Creating LLM provider: deepseek ---"
curl -s -X POST "$API_BASE/api/v1/$TENANT/llm/providers" -H 'Content-Type: application/json' -d "{
  \"type\": \"deepseek\",
  \"enabled\": true,
  \"provider\": \"deepseek\",
  \"endpoint\": \"https://api.deepseek.com\",
  \"apikey\": \"$DEEPSEEK_KEY\",
  \"timeout_ms\": 120000,
  \"model\": \"deepseek-chat\",
  \"models\": [
    {\"name\": \"deepseek-chat\", \"temperature\": 0.3, \"topk\": 0},
    {\"name\": \"deepseek-reasoner\", \"temperature\": 0.3, \"topk\": 0}
  ]
}" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  deepseek: type={d.get(\"type\",\"?\")} id={d.get(\"id\",\"?\")}')"

# ── 3. Agents ────────────────────────────────────────────
echo "--- Creating agents ---"

create_agent() {
  local name="$1" model="$2" max_tokens="$3" temp="${4:-0.3}" soul="$5" instruction="$6"
  curl -s -X POST "$API_BASE/api/v1/$TENANT/agents" -H 'Content-Type: application/json' \
    -d "$(python3 -c "
import json
print(json.dumps({
  'name': '$name',
  'model': '$model',
  'max_tokens': $max_tokens,
  'temperature': $temp,
  'soul': '''$soul''',
  'instruction': '''$instruction'''
}))
")" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  $name: {d.get(\"name\",\"?\")}')"
}

create_agent "supervisor" "deepseek/deepseek-chat" 16384 0.2 \
  "You are a senior autonomous orchestration controller. Output ONLY a JSON object." \
  'Decide the next action:
- "continue" if everything looks good, proceed to next node
- "retry" if a node should be re-executed
- "inject" to add extra review
- "abort" if execution is unsafe
Reply with ONLY JSON: {"action":"continue","target":"","reason":"brief explanation"}'

create_agent "issue-detector" "deepseek/deepseek-chat" 16384 0.3 \
  "You are a senior DevSecOps expert specializing in SAST/DAST/FOSS analysis." \
  'Parse scan results. Normalize issues. Deduplicate. Assign severity.
Output STRICT JSON: {"issues":[{"id":"string","repo":"string","severity":"high|medium|low","type":"sast|dast|dependency","file":"string","description":"string"}]}'

create_agent "fixer-agent" "deepseek/deepseek-chat" 16384 0.3 \
  "You are a secure coding expert." \
  'Fix vulnerabilities safely. Do NOT break business logic.
Output STRICT JSON: {"patches":[{"file":"string","patch":"diff content"}]}'

create_agent "security-reviewer" "deepseek/deepseek-chat" 16384 0.3 \
  "You are a strict security reviewer." \
  'Output JSON: {"decision":true,"confidence":0.9,"risk_level":"low","reason":"string"}'

create_agent "quality-reviewer" "deepseek/deepseek-chat" 16384 0.3 \
  "You are a code quality reviewer." \
  'Output JSON: {"decision":true,"confidence":0.9,"reason":"string"}'

create_agent "arch-reviewer" "deepseek/deepseek-chat" 16384 0.3 \
  "You are an architecture reviewer." \
  'Output JSON: {"decision":true,"confidence":0.9,"reason":"string"}'

# ── 4. Flow ───────────────────────────────────────────────
echo "--- Creating security-autonomy-fixer-v2 flow ---"
FLOW_YAML=$(python3 -c "
import yaml, json, sys
with open('/app/examples/security-autonomy-fixer/flows/security-autonomy-fixer-v2.yaml') as f:
    data = yaml.safe_load(f)
print(json.dumps(data))
" 2>/dev/null || echo "")

if [ -n "$FLOW_YAML" ]; then
  echo "$FLOW_YAML" | python3 -c "
import sys, json
d = json.loads(sys.stdin.read())
# The Create handler expects the flow definition directly
print(json.dumps(d))
" | curl -s -X POST "$API_BASE/api/v1/$TENANT/agentflows" -H 'Content-Type: application/json' -d @-
  echo "  Flow created."
else
  echo "  SKIPPED: yaml file not found or no python3-yaml module"
fi

echo ""
echo "=== Seed complete ==="
echo "Verify: curl $API_BASE/api/v1/$TENANT/mcp"
echo "Verify: curl $API_BASE/api/v1/$TENANT/agents"
echo "Verify: curl $API_BASE/api/v1/$TENANT/agentflows"
