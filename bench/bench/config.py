"""Constants and cluster config loading."""

import logging
import os
import sys

# SFU and client types
SFUS = ["janus", "jitsi", "livekit"]
CLIENTS = ["callzip", "webrtcperf", "chromium"]

# WebRTCPerf
WRP_IMAGE = "ghcr.io/vpalmisano/webrtcperf:devel"
WRP_CONTAINER_NAME = "bench-wrp"
WEB_CONTAINER_NAME = "bench-web"

# Default health thresholds (overridable via cluster config)
MIN_BITRATE_BPS = 2_800_000  # 2.8 Mbps
MIN_FPS = 20.0
VIDEO_BITRATE_MBPS = 3.5     # VP9 test video bitrate (used for NIC ceiling calc)

# Multi-room: max viewers per room before splitting into additional rooms.
VIEWERS_PER_ROOM = {
    "janus": 20,
    "jitsi": 20,
}

# Default timing (overridable via cluster config)
RENDEZVOUS_LEAD_S = 90
EXPERIMENT_DURATION_S = 300
WARMUP_S = 90
TEARDOWN_WAIT_S = 10

# Paths on remote machines
REMOTE_STATS_DIR = "/dev/shm/bench-stats"
REMOTE_LOG_DIR = "/tmp/bench-logs"
REMOTE_IVF_DIR = "/opt/ivf-videos"

# call.zip Docker image and container names
CALLZIP_IMAGE = "callzip:latest"
CALLZIP_SENDER_CONTAINER = "bench-sender"
CALLZIP_VIEWER_CONTAINER = "bench-viewer"

# Binary search bounds
DEFAULT_MIN_R = 1
DEFAULT_MAX_R = 5000

# Paths relative to cwd (bench/ is the working directory)
RESULTS_BASE = "results"
CALLZIP_CONFIGS_DIR = "callzip-configs"
SENDER_CONFIG = os.path.join(CALLZIP_CONFIGS_DIR, "bench-sender.yml")
VIEWER_CONFIGS = {
    "janus": os.path.join(CALLZIP_CONFIGS_DIR, "bench-janus.yml"),
    "jitsi": os.path.join(CALLZIP_CONFIGS_DIR, "bench-jitsi.yml"),
    "livekit": os.path.join(CALLZIP_CONFIGS_DIR, "bench-livekit.yml"),
}
HINTS_FILE = "hints.json"

# Docker compose project names
COMPOSE_PROJECTS = {
    "janus": "callzip-janus",
    "jitsi": "callzip-jitsi",
    "livekit": "callzip-livekit",
}

COMPOSE_PROFILES = {
    "janus": "janus",
    "jitsi": "jitsi",
    "livekit": "livekit",
}

# Jitsi container images
JITSI_IMAGE_TAG = "stable-9646"

# SFU container name
SFU_CONTAINER_NAME = "bench-sfu"

log = logging.getLogger("bench")


def load_cluster_config(path):
    """Load cluster config YAML. Returns dict with normalized fields."""
    import bench.config as cfg

    try:
        import yaml
    except ImportError:
        sys.exit("ERROR: PyYAML is required. Install with: pip install pyyaml")

    with open(path) as f:
        data = yaml.safe_load(f)

    required = ["ssh_key", "ssh_user", "sender", "sfu", "receivers"]
    for key in required:
        if key not in data:
            sys.exit(f"ERROR: cluster config missing required key: {key}")

    if isinstance(data["sfu"], str):
        data["sfu"] = [data["sfu"]]
    if isinstance(data["receivers"], str):
        data["receivers"] = [data["receivers"]]

    # Apply optional overrides
    if "min_bitrate_bps" in data:
        cfg.MIN_BITRATE_BPS = int(data["min_bitrate_bps"])
    if "min_fps" in data:
        cfg.MIN_FPS = float(data["min_fps"])
    if "start_lead_seconds" in data:
        cfg.RENDEZVOUS_LEAD_S = int(data["start_lead_seconds"])
    if "steady_state_seconds" in data:
        cfg.EXPERIMENT_DURATION_S = int(data["steady_state_seconds"])
    if "warmup_seconds" in data:
        cfg.WARMUP_S = int(data["warmup_seconds"])

    return data
