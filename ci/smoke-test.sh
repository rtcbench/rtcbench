#!/bin/sh
# smoke-test.sh - Run call.zip and pass only when a success pattern appears in output.
#
# Usage: smoke-test.sh <config-file> <success-pattern> [timeout-seconds] [min-active-streams]
# Exit:  0 on success, 1 on failure or timeout.

set -eu

CONFIG_FILE="${1:?Usage: smoke-test.sh <config> <pattern> [timeout] [min-active]}"
SUCCESS_PATTERN="${2:?Usage: smoke-test.sh <config> <pattern> [timeout] [min-active]}"
TIMEOUT="${3:-120}"
MIN_ACTIVE="${4:-1}"

LOG_FILE=/tmp/callzip-smoke.log
HEALTH_FILE=/tmp/callzip-health.json
CONFIG_NAME=$(basename "${CONFIG_FILE}" .yml)

mkdir -p /var/log/call-zip /test-videos

# Generate a minimal VP9/IVF test video if not already present.
if [ ! -f /test-videos/test.ivf ]; then
    echo "[smoke-test] Generating test VP9 IVF video..."
    ffmpeg \
        -f lavfi -i "testsrc2=size=1920x1080:rate=25" \
        -t 10 \
        -c:v libvpx-vp9 -b:v 3.5M -minrate 3M -maxrate 4M -deadline realtime \
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

# check_health URL
# Prints a summary line and returns 0 when status=="ok" and every active
# viewer (seen within 30s) has both smooth_bitrate_bps > 0 and smooth_fps > 0.
check_health() {
    RESP=$(curl -sf "$1" 2>/dev/null) || return 1
    echo "${RESP}" | jq -r '
        "[health] status=\(.status) viewers_active=\(.viewers_active)/\(.viewers_total) aggregate=\(.aggregate_bitrate_mbps)Mbps",
        (.viewers[] | "  \(.nickname): \(.smooth_bitrate_bps/1000 | . * 10 | round / 10)kbps \(.smooth_fps | . * 10 | round / 10)fps samples=\(.sample_count) last_seen=\(.last_seen_ago_ms)ms")
    '
    echo "${RESP}" | jq -e --argjson min "${MIN_ACTIVE}" '
        .status == "ok" and
        .viewers_active >= $min and
        ([.viewers[] | select(.last_seen_ago_ms <= 30000)] |
            length >= $min and all(.smooth_bitrate_bps >= 3000000 and .smooth_fps > 0))
    ' > /dev/null 2>&1 || return 1
    echo "${RESP}" > "${HEALTH_FILE}"
}

# print_summary - print a results table from the last successful health response.
print_summary() {
    [ -f "${HEALTH_FILE}" ] || return 0
    echo ""
    SUMMARY=$(jq -r --arg name "${CONFIG_NAME}" '
        "[smoke-test] \($name) results:",
        "  receiver | sender ssrc | bitrate | fps",
        "  --- | --- | --- | ---",
        (.viewers[] |
            (.nickname | split("[")[0]) as $receiver |
            (.nickname | split("[")[1] | rtrimstr("]")) as $ssrc |
            "  \($receiver) | \($ssrc) | \(.smooth_bitrate_bps/1000 | . * 10 | round / 10) kbps | \(.smooth_fps | . * 10 | round / 10) fps"),
        "  total (\(.viewers_active)/\(.viewers_total) active) | | \(.aggregate_bitrate_mbps | . * 100 | round / 100) Mbps |"
    ' "${HEALTH_FILE}")
    echo "${SUMMARY}"
    if [ -d /results ]; then
        printf '%s\n\n' "${SUMMARY}" > "/results/${CONFIG_NAME}.txt"
    fi
}

ELAPSED=0
while [ "${ELAPSED}" -lt "${TIMEOUT}" ]; do
    case "${SUCCESS_PATTERN}" in
        http://*)
            if check_health "${SUCCESS_PATTERN}"; then
                echo ""
                echo "[smoke-test] SUCCESS - health endpoint returned ok after ${ELAPSED}s"
                kill "${TAIL_PID}" 2>/dev/null || true
                kill "${CALLZIP_PID}" 2>/dev/null || true
                wait "${CALLZIP_PID}" 2>/dev/null || true
                print_summary
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
