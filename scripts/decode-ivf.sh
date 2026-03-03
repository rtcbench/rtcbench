#!/bin/bash
set -euo pipefail

usage() {
  echo "Usage: $0 /path/to/folder" >&2
  exit 1
}

[[ $# -eq 1 ]] || usage

INPUT_DIR="$1"
INPUT_DIR="${INPUT_DIR/#\~/$HOME}"

if [[ ! -d "$INPUT_DIR" ]]; then
  echo "Error: '$INPUT_DIR' is not a directory." >&2
  exit 2
fi

shopt -s nullglob

ivf_files=("$INPUT_DIR"/*.ivf)

if (( ${#ivf_files[@]} == 0 )); then
  echo "No .ivf files found in: $INPUT_DIR"
  exit 0
fi

for f in "${ivf_files[@]}"; do
  base="$(basename "$f")"
  out_dir="$INPUT_DIR/${base}-decoded"
  mkdir -p "$out_dir"
  echo "Decoding $f -> $out_dir/"
  ffmpeg -y -vsync 0 -i "$f" "$out_dir/${base%.ivf}-%04d.png"
done
