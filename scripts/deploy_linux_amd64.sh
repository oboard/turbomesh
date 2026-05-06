#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="sz.oboard.fun"
REMOTE_DIR="/home/oboard/turbomesh"
BINARY_NAME="turbomesh-linux-amd64"
REMOTE_TMP="/tmp/$BINARY_NAME.$$"

cd "$ROOT_DIR"

echo "Building frontend..."
vp build

echo "Building Linux amd64 binary..."
GOOS=linux GOARCH=amd64 go build -o "$BINARY_NAME" .

echo "Ensuring remote directory exists..."
ssh -t "$REMOTE_HOST" "sudo mkdir -p '$REMOTE_DIR' && sudo chown oboard:oboard '$REMOTE_DIR'"

echo "Uploading dist and binary..."
rsync -az --delete dist/ "$REMOTE_HOST:$REMOTE_DIR/dist/"
scp "$BINARY_NAME" "$REMOTE_HOST:$REMOTE_TMP"

echo "Restarting turbomesh.service..."
ssh -t "$REMOTE_HOST" "sudo install -m 0755 '$REMOTE_TMP' '$REMOTE_DIR/$BINARY_NAME' && rm -f '$REMOTE_TMP' && sudo systemctl restart turbomesh.service && sudo systemctl status turbomesh.service --no-pager"
