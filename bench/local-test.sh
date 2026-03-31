#!/bin/bash
# Local sender bench test - everything on this machine, no SSH needed.
# Usage: ./local-test.sh [NUM_SENDERS]
set -euo pipefail

S=${1:-20}
STATS_DIR=/dev/shm/bench-stats-local
LOG_DIR=/tmp/bench-logs-local
IVF_DIR=$(cd "$(dirname "$0")" && pwd)/ivf
BIN=$(cd "$(dirname "$0")" && pwd)/binaries/rtcbench
VIEWER_CFG=/tmp/bench-viewer-local.yml
SENDER_CFG=/tmp/bench-sender-local.yml
LK_CFG=/tmp/bench-livekit-local.yaml

cleanup() {
    echo "Cleaning up..."
    [[ -n "${VIEWER_PID:-}" ]] && kill -- -$VIEWER_PID 2>/dev/null || kill $VIEWER_PID 2>/dev/null || true
    [[ -n "${SENDER_PID:-}" ]] && kill -- -$SENDER_PID 2>/dev/null || kill $SENDER_PID 2>/dev/null || true
    docker rm -f bench-livekit-local 2>/dev/null || true
}
trap cleanup EXIT

mkdir -p "$STATS_DIR" "$LOG_DIR"
rm -f "$STATS_DIR"/*.jsonl

cat > "$LK_CFG" <<EOF
port: 7880
rtc:
  port_range_start: 20000
  port_range_end: 30000
  use_external_ip: false
  node_ip: 127.0.0.1
  congestion_control:
    stream_allocator:
      min_channel_capacity: 10000000000
keys:
  devkey: secret
room:
  auto_create: true
  departure_timeout: 5
  empty_timeout: 10
logging:
  level: warn
EOF

cat > "$VIEWER_CFG" <<EOF
apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: bench-local-viewer
spec:
  plugin: livekit
  conference:
    name: room-local
    usersPerRoom: 1
    totalRooms: 1
    cameras:
      perRoom: 0
    joinPolicy:
      concurrency: 1
  pluginConfig:
    livekit:
      wsURL: 'ws://127.0.0.1:7880'
      apiKey: devkey
      apiSecret: secret
  network:
    serverIP: 127.0.0.1
  metrics:
    port: 9091
    statsJSONLPath: ${STATS_DIR}
  logging:
    console: false
    directory: ${LOG_DIR}
    streams:
      - name: general
        level: info
      - name: video_stats
        level: info
      - name: signaling
        level: info
      - name: media
        level: info
      - name: packets
        level: info
EOF

cat > "$SENDER_CFG" <<EOF
apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: bench-local-senders
spec:
  plugin: livekit
  conference:
    name: room-local
    usersPerRoom: ${S}
    totalRooms: 1
    cameras:
      perRoom: ${S}
      fileType: ivf
      videoCodec: vp9
      directory: ${IVF_DIR}
      inMemory: true
    joinPolicy:
      concurrency: ${S}
  pluginConfig:
    livekit:
      wsURL: 'ws://127.0.0.1:7880'
      apiKey: devkey
      apiSecret: secret
  network:
    serverIP: 127.0.0.1
  logging:
    console: false
    directory: ${LOG_DIR}
    streams:
      - name: general
        level: error
      - name: signaling
        level: error
      - name: media
        level: error
      - name: packets
        level: error
      - name: video_stats
        level: error
EOF

echo "Starting LiveKit SFU (cores 4-7)..."
docker rm -f bench-livekit-local 2>/dev/null || true
docker run -d --name bench-livekit-local --network host \
    --cpuset-cpus 6-9 \
    -v "$LK_CFG:$LK_CFG:ro" \
    rtcbench-livekit:latest --config "$LK_CFG"
for i in $(seq 20); do
    wget -qO- http://localhost:7880/ >/dev/null 2>&1 && break || sleep 1
done
echo "LiveKit up."

echo "Starting viewer (cores 8-11)..."
ulimit -n 65536
taskset -c 10-13 "$BIN" "$VIEWER_CFG" &
VIEWER_PID=$!
sleep 8

echo "Starting $S senders (cores 0-3)..."
taskset -c 0-5 "$BIN" "$SENDER_CFG" &
SENDER_PID=$!

echo "Waiting 40s (10s join + 10s warmup + 20s steady)..."
sleep 40

echo ""
echo "=== Results: $S senders, no subscription cap ==="
perl -e '
my %last;
for my $f (sort glob("'"$STATS_DIR"'/*.jsonl")) {
    open my $fh,"<",$f or next;
    while (my $line = <$fh>) {
        next unless $line =~ /\S/;
        my ($v) = $line =~ /"viewer":"([^"]+)"/; next unless $v;
        my ($b) = $line =~ /"bitrate_bps":([0-9.eE+-]+)/; $b //= 0;
        my ($p) = $line =~ /"fps":([0-9.eE+-]+)/; $p //= 0;
        $last{$v} = { b => $b, p => $p };
    }
}
my $total   = scalar keys %last;
my $healthy = grep { $_->{b} >= 2800000 && $_->{p} >= 20 } values %last;
printf "Tracks subscribed: %d / %d senders\n", $total, '"$S"';
printf "Healthy (>=2.8 Mbps, >=20 fps): %d/%d\n", $healthy, $total;
'
