#!/bin/bash
# Deploy web frontend to remote server
# Usage: ./scripts/deploy-web.sh

set -e

REMOTE_HOST="${SENTINEL_DEPLOY_HOST:?Set SENTINEL_DEPLOY_HOST env var}"
REMOTE_PATH="${SENTINEL_DEPLOY_PATH:?Set SENTINEL_DEPLOY_PATH env var}"
LOCAL_PATH="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Building web frontend ==="
cd "$LOCAL_PATH"
npm run build:web

echo "=== Cleaning old assets on remote ==="
ssh $REMOTE_HOST "cd /d $REMOTE_PATH/dist/web/assets && del /Q *.js *.css 2>nul || echo 'No old assets to clean'"

echo "=== Deploying to remote server ==="
scp -r "$LOCAL_PATH/dist/web/"* "$REMOTE_HOST:$REMOTE_PATH/dist/web/"

echo "=== Verifying deployment ==="
ssh $REMOTE_HOST "docker exec sentinel-frontend cat /usr/share/nginx/html/index.html | findstr index-"

echo ""
echo "=== Deployment complete ==="
echo "Clear browser cache (Ctrl+Shift+R) to see changes"
