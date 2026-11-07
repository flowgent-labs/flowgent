#!/bin/bash
# ═══════════════════════════════════════════════════════════════
# nexus3-iq.sh — Nexus3 + SonatypeIQ dependency firewall check
#
# Subcommands:
#   get-available-versions <groupId> <artifactId> [currentVersion]
#     Returns top-N non-quarantined versions. Queries both Nexus3
#     REST API and SonatypeIQ Web UI API, merges firewall status,
#     filters quarantined versions, returns recommended + allowed.
#
# Design: docs/.agent/nexus3-iq-skill-design.md
# ═══════════════════════════════════════════════════════════════
set -euo pipefail

main() {
  case "${1:-}" in
    get-available-versions) shift; cmd_get_versions "$@";;
    *) echo "usage: $0 get-available-versions <groupId> <artifactId> [currentVersion]"; exit 1;;
  esac
}

cmd_get_versions() {
  local gid="${1:-}"; local aid="${2:-}"; local cur="${3:-}"
  [ -z "$gid" ] || [ -z "$aid" ] && { echo '{"error":"groupId and artifactId required"}'; exit 1; }

  local n3url="${NEXUS3_URL:-http://nexus3:8081}"
  local n3user="${NEXUS3_USER:-admin}"; local n3pass="${NEXUS3_PASSWORD:-admin}"
  local iqurl="${SONATYPEIQ_URL:-http://sonatypeiq:8070}"
  local iquser="${SONATYPEIQ_USER:-}"; local iqpass="${SONATYPEIQ_PASS:-}"
  local topn="${TOP_N:-5}"

  # Fetch Nexus3 versions
  local nx
  nx=$(curl -s --noproxy '*' -u "${n3user}:${n3pass}" \
    "$n3url/service/rest/v1/search?group=$gid&name=$aid&sort=version&direction=desc" \
    2>/dev/null || echo '{"items":[]}')

  # Fetch SonatypeIQ firewall status (NON-swagger Web UI API)
  local iq_fw='{}'
  [ -n "$iquser" ] && iq_fw=$(curl -s --noproxy '*' -u "${iquser}:${iqpass}" \
    "$iqurl/api/v2/components/firewall?group=$gid&name=$aid" 2>/dev/null || echo '{}')

  # Fetch SonatypeIQ upgrade recommendations
  local iq_up='{}'
  [ -n "$iquser" ] && [ -n "$cur" ] && iq_up=$(curl -s --noproxy '*' -u "${iquser}:${iqpass}" \
    "$iqurl/api/v2/components/upgrade?group=$gid&name=$aid&version=$cur" 2>/dev/null || echo '{}')

  # Merge + filter in Python
  TOPN="$topn" CURVER="$cur" NEXUS3_JSON="$nx" IQ_FIREWALL="$iq_fw" IQ_UPGRADE="$iq_up" python3 -c "
import sys,os,json

nx=json.loads(os.environ['NEXUS3_JSON'])
iq_fw=json.loads(os.environ.get('IQ_FIREWALL','{}'))
iq_up=json.loads(os.environ.get('IQ_UPGRADE','{}'))
cur=os.environ.get('CURVER','')
topn=int(os.environ.get('TOPN','5'))

# Nexus3 versions (filter snapshots + current)
items=nx.get('items',[])
versions=[i['version'] for i in items if 'SNAPSHOT' not in i.get('version','').upper() and i.get('version')!=cur]

# IQ firewall status per version
fw_map={}
for c in iq_fw.get('components',[]): fw_map[c.get('version','')]=c.get('firewallStatus','unknown')

# IQ recommended versions
rec=set()
for r in iq_up.get('recommendations',[]): rec.add(r.get('version',''))

result=[]
for v in versions:
    fw=fw_map.get(v,'unknown')
    if fw=='deny': continue
    result.append({'version':v,'firewall':fw,'recommended':v in rec,'source':'iq' if fw!='unknown' else 'nexus3'})

result.sort(key=lambda x:(not x['recommended'],x['firewall']!='allow',x['version']),reverse=False)
result=result[:topn]

warn='' if result else 'All versions quarantined. Admin waive required in SonatypeIQ UI.'
print(json.dumps({'versions':result,'warning':warn,'total':len(result)}))
" 2>/dev/null || echo '{"versions":[],"warning":"processing error","total":0}'
}

main "$@"
