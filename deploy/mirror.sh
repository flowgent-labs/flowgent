#!/bin/bash
# Mirror Docker Hub images through CN jump host → Aliyun CR → local k3s.
# Usage: ./deploy/mirror.sh <docker-image> [aliyun-repo-name] [jump-host]
#
# Examples:
#   ./deploy/mirror.sh rancher/mirrored-pause:3.6
#   ./deploy/mirror.sh alpine:3.21 alpine
#   ./deploy/mirror.sh golang:1.25 golang  root@8.219.71.91
set -euo pipefail

IMAGE="${1:?Usage: $0 <docker-image> [aliyun-repo] [jump-host]}"
REPO="${2:-$(echo "$IMAGE" | sed 's/[\/:]/_/g')}"
TAG=$(echo "$IMAGE" | rev | cut -d: -f1 | rev)
[ -z "$TAG" ] && TAG="latest"
JUMP_HOST="${3:-root@8.219.71.91}"
NS="${MIRROR_NS:-wl4g}"
REG="${MIRROR_REG:-registry.cn-shenzhen.aliyuncs.com}"
ALIYUN="${REG}/${NS}/${REPO}:${TAG}"

echo "=== Mirror: $IMAGE → $ALIYUN ==="

echo "[1/4] Pull on jump host + push to Aliyun CR..."
ssh "$JUMP_HOST" "docker pull '$IMAGE' && docker tag '$IMAGE' '$ALIYUN' && docker push '$ALIYUN'" || {
  echo "ERROR: SSH/push failed" >&2; exit 1
}

echo "[2/4] Pull from Aliyun CR locally..."
sudo docker pull "$ALIYUN" || { echo "ERROR: local pull failed" >&2; exit 1; }

echo "[3/4] Tag for k3s..."
sudo docker tag "$ALIYUN" "docker.io/$IMAGE"

echo "[4/4] Import into k3s containerd..."
sudo docker save "docker.io/$IMAGE" | sudo k3s ctr images import -

echo "=== Done: $IMAGE available in k3s ==="
