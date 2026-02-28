# Sonatype IQ CLI 自动扫描脚本
# 用法: bash scan-rengine-foss.sh [applicationId]
set -euo pipefail

IQ_SERVER_URL="http://localhost:8070"
IQ_USER="admin"
IQ_PASS="admin123"
APP_ID="${1:-rengine}"
PROJECT_DIR="/root/rengine"

echo "=== Rengine FOSS Scan (Sonatype IQ) ==="
echo ">> Server: $IQ_SERVER_URL"
echo ">> Application: $APP_ID"

# Check if IQ CLI is available
if command -v clm &> /dev/null; then
    CLM_CMD="clm"
else
    echo "ERROR: Sonatype IQ CLI (clm) not found"
    echo "Install: curl -fsSL https://raw.githubusercontent.com/wl4g/cyberbot/main/.agent/scripts/install-iq-cli.sh | bash"
    exit 1
fi

echo ">> Scanning Maven project..."
$CLM_CMD analyze \
  -s compile \
  -t compile \
  -a "$APP_ID" \
  -u "${IQ_USER}:${IQ_PASS}" \
  --server "$IQ_SERVER_URL" \
  -i "$PROJECT_DIR" \
  2>&1

echo "=== Scan complete ==="
