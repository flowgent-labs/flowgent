#!/bin/bash
set -euo pipefail
MODE="${1:-}"
LOG="/tmp/mcp-${MODE}.log"
echo "$(date): MCP mock started, mode=$MODE" >> "$LOG"
while IFS= read -r line; do
  echo "$(date): RECV $line" >> "$LOG"
  METHOD=$(echo "$line" | jq -r '.method // ""' 2>/dev/null || echo "")
  ID=$(echo "$line" | jq -r '.id // ""' 2>/dev/null || echo "")

  case "$METHOD" in
    initialize)
      echo "{\"jsonrpc\":\"2.0\",\"id\":$ID,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{\"tools\":{}},\"serverInfo\":{\"name\":\"${MODE}-mock\",\"version\":\"1.0\"}}}"
      ;;
    tools/list)
      case "$MODE" in
        github)
          echo "{\"jsonrpc\":\"2.0\",\"id\":$ID,\"result\":{\"tools\":[{\"name\":\"get_latest_commit\",\"description\":\"Get latest commit info\",\"inputSchema\":{\"type\":\"object\",\"properties\":{\"repo\":{\"type\":\"string\"},\"branch\":{\"type\":\"string\"}}}}]}}"
          ;;
        sonarqube)
          echo "{\"jsonrpc\":\"2.0\",\"id\":$ID,\"result\":{\"tools\":[{\"name\":\"get_issues\",\"description\":\"Get SonarQube issues\",\"inputSchema\":{\"type\":\"object\",\"properties\":{\"severities\":{\"type\":\"string\"},\"project_key\":{\"type\":\"string\"}}}}]}}"
          ;;
        sonatype-iq)
          echo "{\"jsonrpc\":\"2.0\",\"id\":$ID,\"result\":{\"tools\":[{\"name\":\"get_jobs_by_commit\",\"description\":\"Get IQ jobs by commit\",\"inputSchema\":{\"type\":\"object\",\"properties\":{\"commit_sha\":{\"type\":\"string\"},\"repo\":{\"type\":\"string\"}}}}]}}"
          ;;
        *)
          echo "{\"jsonrpc\":\"2.0\",\"id\":$ID,\"result\":{\"tools\":[{\"name\":\"mock_tool\",\"description\":\"Mock tool\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}]}}"
          ;;
      esac
      ;;
    tools/call)
      echo "{\"jsonrpc\":\"2.0\",\"id\":$ID,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"status\\\":\\\"success\\\",\\\"message\\\":\\\"mock response from ${MODE}\\\"}\"}]}}"
      ;;
    notifications/*|notifications/*)
      # No response for notifications
      ;;
  esac
done
