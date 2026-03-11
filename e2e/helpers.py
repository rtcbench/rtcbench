"""
Shared helpers for call.zip e2e tests.

Infrastructure (janus/jitsi) runs in Docker Compose (fixtures in conftest.py).
call.zip itself runs as a Docker container per test, joined to the infra network.
"""
import contextlib
import socket
import subprocess
import tempfile
import time
import uuid
from pathlib import Path

import requests
import yaml

REPO_ROOT = Path(__file__).parent.parent

# Each plugin gets its own compose project so parallel runs don't fight over
# the same project state or network.
JANUS_PROJECT = "callzip-janus"
JITSI_PROJECT = "callzip-jitsi"
LIVEKIT_PROJECT = "callzip-livekit"
JANUS_NETWORK = f"{JANUS_PROJECT}_callzip-net"              # 172.20.0.0/24
JITSI_NETWORK = f"{JITSI_PROJECT}_callzip-jitsi-net"       # 172.21.0.0/24
LIVEKIT_NETWORK = f"{LIVEKIT_PROJECT}_callzip-livekit-net"  # 172.22.0.0/24

# Accumulated delivery results for the terminal summary (populated by record_result).
_delivery_results: list[tuple[str, dict]] = []


def get_delivery_results() -> list[tuple[str, dict]]:
    return _delivery_results


def record_result(scenario: str, data: dict) -> None:
    _delivery_results.append((scenario, data))


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("", 0))
        return s.getsockname()[1]


def _compose(*args, project: str, profiles: tuple = ()) -> list[str]:
    cmd = ["docker", "compose", "--project-name", project]
    for p in profiles:
        cmd += ["--profile", p]
    return cmd + list(args)


def poll_health(
    url: str,
    timeout: int,
    min_active: int,
    min_bitrate_bps: int = 3_000_000,
) -> dict:
    """
    Poll GET url until all conditions are met:
      - status == "ok"
      - at least min_active viewers seen within 30 s
      - every active viewer has smooth_bitrate_bps >= min_bitrate_bps and smooth_fps > 0

    Returns the last passing health payload.
    Raises TimeoutError if the conditions are not met within timeout seconds.
    """
    deadline = time.time() + timeout
    last_err = "no response yet"
    while time.time() < deadline:
        try:
            r = requests.get(url, timeout=2)
            data = r.json()
            active = [v for v in data["viewers"] if v["last_seen_ago_ms"] <= 30_000]
            if (
                data["status"] == "ok"
                and len(active) >= min_active
                and all(v["smooth_bitrate_bps"] >= min_bitrate_bps for v in active)
                and all(v["smooth_fps"] > 0 for v in active)
            ):
                return data
            last_err = (
                f"status={data['status']} "
                f"active={len(active)}/{min_active} "
                f"bitrates=[{', '.join(str(round(v['smooth_bitrate_bps'] / 1000)) + 'kbps' for v in active)}]"
            )
        except Exception as exc:
            last_err = str(exc)
        time.sleep(2)
    raise TimeoutError(f"health check did not pass within {timeout}s — last: {last_err}")


@contextlib.contextmanager
def callzip_run(config, video_dir: Path, network: str, recording_dir: Path = None):
    """
    Start call.zip in a Docker container joined to the given Docker network.

    config: str filename (resolved under ci/, e.g. "janus-smoke.yml")
            OR absolute Path to a YAML file (used for recording tests with overrides).
    video_dir: host directory mounted read-only as /test-videos inside the container.
    network: Docker network name to join (JANUS_NETWORK or JITSI_NETWORK).
    recording_dir: if given, mounted as /tmp/recordings (write).

    Yields (health_url, proc) where health_url is http://localhost:<port>/health.
    On exit: stops the container, waits for the process, prints captured output.
    """
    host_port = _free_port()
    name = f"callzip-e2e-{uuid.uuid4().hex[:8]}"
    config = Path(config)

    if config.is_absolute():
        ci_mounts = ["-v", f"{config}:/tmp/e2e-config.yml:ro"]
        container_config = "/tmp/e2e-config.yml"
    else:
        ci_mounts = ["-v", f"{REPO_ROOT / 'ci'}:/ci:ro"]
        container_config = f"/ci/{config}"

    cmd = [
        "docker", "run", "--rm", "--name", name,
        "--network", network,
        "-p", f"{host_port}:9090",
        "-v", f"{video_dir}:/test-videos:ro",
        *ci_mounts,
    ]
    if recording_dir is not None:
        cmd += ["-v", f"{recording_dir}:/tmp/recordings"]
    cmd += ["callzip:latest", container_config]

    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    try:
        yield f"http://localhost:{host_port}/health", proc
    finally:
        subprocess.run(["docker", "stop", name], capture_output=True, timeout=15)
        out, _ = proc.communicate(timeout=15)
        if out:
            print(f"\n--- callzip output ({config.name}) ---\n{out}---")


def build_recording_config(base_config_name: str) -> Path:
    """
    Write a temporary YAML config identical to ci/<base_config_name> but with
    recording.enabled=true and recording.directory=/tmp/recordings.

    Returns the path to the temp file. Caller is responsible for unlinking it.
    """
    with open(REPO_ROOT / "ci" / base_config_name) as f:
        cfg = yaml.safe_load(f)
    cfg["spec"]["conference"]["recording"]["enabled"] = True
    cfg["spec"]["conference"]["recording"]["directory"] = "/tmp/recordings"
    tmp = tempfile.NamedTemporaryFile(
        suffix=".yml", delete=False, mode="w", prefix="callzip-e2e-", dir="/tmp"
    )
    yaml.dump(cfg, tmp)
    tmp.close()
    return Path(tmp.name)
