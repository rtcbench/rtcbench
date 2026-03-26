#!/usr/bin/env bash
# Generate a VP9 SVC IVF file from 1080p30.mp4.
#
# Produces 3 spatial layers (270p / 540p / 1080p) × 3 temporal layers
# at ~3.5 Mbps aggregate using vpxenc.
#
# Prerequisites:
#   - ffmpeg (for MP4 → Y4M conversion)
#   - vpxenc (from libvpx-tools / libvpx)
#
# Usage:
#   ./generate-svc-ivf.sh [input.mp4] [output.ivf]
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
MP4="${1:-$DIR/../videos/1080p30.mp4}"
IVF="${2:-$DIR/../videos/svc-1080p30.ivf}"

if [ -f "$IVF" ]; then
    echo "exists: $IVF ($(du -h "$IVF" | cut -f1))"
    exit 0
fi

for cmd in ffmpeg vpxenc; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "error: $cmd not found. Install it first." >&2
        exit 1
    fi
done

[ -f "$MP4" ] || { echo "error: $MP4 not found" >&2; exit 1; }

mkdir -p "$(dirname "$IVF")"

Y4M="$(mktemp -t svc-XXXXXX.y4m)"
trap 'rm -f "$Y4M"' EXIT

echo "Converting $MP4 → Y4M..."
ffmpeg -i "$MP4" -an -pix_fmt yuv420p "$Y4M" -y -loglevel warning

echo "Encoding VP9 SVC (3 spatial × 3 temporal layers, ~3.5 Mbps)..."
vpxenc "$Y4M" -o "$IVF" \
    --codec=vp9 \
    --target-bitrate=3500 \
    --min-q=2 --max-q=52 \
    --end-usage=cbr \
    --svc \
    --svc-num-spatial-layers=3 \
    --svc-num-temporal-layers=3 \
    --svc-bitrates=200,800,3500 \
    --svc-frame-scale-factor=4/1,2/1,1/1 \
    --kf-max-dist=30 \
    -w 1920 -h 1080 --fps=30/1 \
    --passes=1

echo "Created: $IVF ($(du -h "$IVF" | cut -f1))"
