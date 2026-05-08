#!/usr/bin/env bash
# scripts/redeploy.sh — Build, install, and restart oc-go-cc
#
# Usage:
#   ./scripts/redeploy.sh            # defaults
#   BINARY=foo CMD=./cmd/foo ./scripts/redeploy.sh
#
# Environment variables (all optional):
#   BINARY     Binary name              (default: oc-go-cc)
#   CMD        Go build target          (default: ./cmd/oc-go-cc)
#   GOPATHBIN  Install directory        (default: $(go env GOPATH)/bin)
#   HEALTH     Health check URL         (default: http://127.0.0.1:3456/health)
#   TIMEOUT    Health check timeout sec (default: 10)

set -euo pipefail

# ── Defaults ──
BINARY="${BINARY:-oc-go-cc}"
CMD="${CMD:-./cmd/$BINARY}"
GOPATHBIN="${GOPATHBIN:-$(go env GOPATH)/bin}"
HEALTH="${HEALTH:-http://127.0.0.1:3456/health}"
TIMEOUT="${TIMEOUT:-10}"
BINPATH="$GOPATHBIN/$BINARY"

# ── Step 1: Stop ──
echo "[1/5] Stopping $BINARY..."
if command -v "$BINARY" &>/dev/null; then
    "$BINARY" stop 2>/dev/null || true
fi
# Fallback: kill by PID file or process name
pkill -f "$BINPATH" 2>/dev/null || true
sleep 1

# ── Step 2: Build & Install ──
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo "dev")"
echo "[2/5] Building $CMD (version: $VERSION)..."
go install -ldflags "-X main.version=$VERSION" "$CMD"

# ── Step 3: Codesign (macOS) ──
if [[ "$(uname)" == "Darwin" ]]; then
    echo "[3/5] Codesigning $BINPATH..."
    codesign -s - "$BINPATH" 2>/dev/null || true
else
    echo "[3/5] Codesign skipped (not macOS)"
fi

# ── Step 4: Start ──
echo "[4/5] Starting $BINARY..."
"$BINARY" serve -b

# ── Step 5: Health check ──
echo "[5/5] Health check..."
ok=false
for i in $(seq 1 "$TIMEOUT"); do
    if curl -sf -o /dev/null "$HEALTH" 2>/dev/null; then
        ok=true
        break
    fi
    sleep 1
done

if $ok; then
    echo "OK — $BINARY is running ($(curl -sf "$HEALTH" 2>/dev/null || echo "$HEALTH"))"
else
    echo "WARN — health check failed after ${TIMEOUT}s, check logs"
    exit 1
fi
