#!/bin/sh
# smoke-test.sh - Run call.zip and pass only when a success pattern appears in output.
#
# Usage: smoke-test.sh <config-file> <success-pattern> [timeout-seconds]
# Exit:  0 on success, 1 on failure or timeout.

set -eu

CONFIG_FILE="${1:?Usage: smoke-test.sh <config> <pattern> [timeout]}"
SUCCESS_PATTERN="${2:?Usage: smoke-test.sh <config> <pattern> [timeout]}"
TIMEOUT="${3:-120}"

LOG_FILE=/tmp/callzip-smoke.log

mkdir -p /var/log/call-zip /test-videos

# Generate a minimal VP9/IVF test video if not already present.
if [ ! -f /test-videos/test.ivf ]; then
    echo "[smoke-test] Generating test VP9 IVF video..."
    ffmpeg \
        -f lavfi -i "color=black:size=320x240:rate=30" \
        -t 10 \
        -c:v libvpx-vp9 -b:v 200k -deadline realtime \
        -f ivf /test-videos/test.ivf \
        -y -loglevel warning
    echo "[smoke-test] Test video ready: /test-videos/test.ivf"
fi

echo "[smoke-test] Starting call.zip  config=${CONFIG_FILE}"
echo "[smoke-test] Waiting for pattern='${SUCCESS_PATTERN}'  timeout=${TIMEOUT}s"

call.zip "${CONFIG_FILE}" >"${LOG_FILE}" 2>&1 &
CALLZIP_PID=$!

# Mirror the log to container stdout so Docker/CI captures it.
tail -f "${LOG_FILE}" &
TAIL_PID=$!

ELAPSED=0
while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
    # HTTP mode: poll URL and check for "status":"ok"
    case "${SUCCESS_PATTERN}" in
        http://*)
            if curl -sf "${SUCCESS_PATTERN}" 2>/dev/null | grep -q '"status":"ok"'; then
                echo ""
                echo "[smoke-test] SUCCESS - health endpoint returned ok after ${ELAPSED}s"
                kill "${TAIL_PID}" 2>/dev/null || true
                kill "${CALLZIP_PID}" 2>/dev/null || true
                wait "${CALLZIP_PID}" 2>/dev/null || true
                echo "[smoke-test] PASSED"
                exit 0
            fi
            ;;
        *)
            if grep -qF "${SUCCESS_PATTERN}" "${LOG_FILE}" 2>/dev/null; then
                echo ""
                echo "[smoke-test] SUCCESS - found pattern after ${ELAPSED}s"
                kill "${TAIL_PID}" 2>/dev/null || true
                kill "${CALLZIP_PID}" 2>/dev/null || true
                wait "${CALLZIP_PID}" 2>/dev/null || true
                echo "[smoke-test] PASSED"
                exit 0
            fi
            ;;
    esac

    if ! kill -0 "${CALLZIP_PID}" 2>/dev/null; then
        echo ""
        echo "[smoke-test] FAILURE - call.zip exited unexpectedly after ${ELAPSED}s"
        echo "=== output ==="
        cat "${LOG_FILE}"
        kill "${TAIL_PID}" 2>/dev/null || true
        exit 1
    fi

    sleep 2
    ELAPSED=$((ELAPSED + 2))
done

echo ""
echo "[smoke-test] FAILURE - timed out after ${TIMEOUT}s without '${SUCCESS_PATTERN}'"
echo "=== output ==="
cat "${LOG_FILE}"
kill "${TAIL_PID}" 2>/dev/null || true
kill "${CALLZIP_PID}" 2>/dev/null || true
exit 1
