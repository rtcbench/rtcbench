#!/usr/bin/env bash
# Serve the screenshot verification UI on port 8099.
# Usage: ./scripts/serve-screenshots.sh [port]
set -euo pipefail

PORT="${1:-8099}"
DIR="$(cd "$(dirname "$0")/../e2e/screenshots" && pwd)"

# Kill any existing server on the port.
lsof -ti :"$PORT" 2>/dev/null | xargs -r kill 2>/dev/null || true

echo "Serving $DIR on http://0.0.0.0:$PORT"
echo "Open http://$(hostname -I | awk '{print $1}'):$PORT/verify.html"
cd "$DIR" && exec python3 -m http.server "$PORT" --bind 0.0.0.0
