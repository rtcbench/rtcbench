"""
Shared helpers for rtcbench e2e tests.

Infrastructure (janus/jitsi) runs in Docker Compose (fixtures in conftest.py).
rtcbench itself runs as a Docker container per test, joined to the infra network.
"""
import contextlib
import subprocess
import tempfile
import time
import uuid
from pathlib import Path

import requests
import yaml

REPO_ROOT = Path(__file__).parent.parent

# When the test runner itself is in a Docker container (local dev via Makefile),
# published ports are reachable via host.docker.internal, not localhost.
# On CI (native Linux process), localhost works fine.
_HEALTH_HOST = "host.docker.internal" if Path("/.dockerenv").exists() else "localhost"

# Each plugin gets its own compose project so parallel runs don't fight over
# the same project state or network.
JANUS_PROJECT = "rtcbench-janus"
JITSI_PROJECT = "rtcbench-jitsi"
LIVEKIT_PROJECT = "rtcbench-livekit"
MEDIASOUP_PROJECT = "rtcbench-mediasoup"
JANUS_NETWORK = f"{JANUS_PROJECT}_rtcbench-net"                     # 172.20.0.0/24
JITSI_NETWORK = f"{JITSI_PROJECT}_rtcbench-jitsi-net"              # 172.21.0.0/24
LIVEKIT_NETWORK = f"{LIVEKIT_PROJECT}_rtcbench-livekit-net"         # 172.22.0.0/24
MEDIASOUP_NETWORK = f"{MEDIASOUP_PROJECT}_rtcbench-mediasoup-net"   # 172.23.0.0/24

# Environment variables needed by the generic ci/ smoke configs ($env: substitution).
PLUGIN_ENV = {
    "janus":     {"PLUGIN_ID": "janus",     "SERVER_IP": "172.20.0.10"},
    "jitsi":     {"PLUGIN_ID": "jitsi",     "SERVER_IP": "172.21.0.20"},
    "livekit":   {"PLUGIN_ID": "livekit",   "SERVER_IP": "172.22.0.10"},
    "mediasoup": {"PLUGIN_ID": "mediasoup", "SERVER_IP": "172.23.0.10"},
}

# Accumulated delivery results for the terminal summary (populated by record_result).
_delivery_results: list[tuple[str, dict]] = []
_svc_results: list[tuple[str, dict]] = []


def get_delivery_results() -> list[tuple[str, dict]]:
    return _delivery_results


def get_svc_results() -> list[tuple[str, dict]]:
    return _svc_results


def record_result(scenario: str, data: dict) -> None:
    _delivery_results.append((scenario, data))


def record_svc_result(scenario: str, data: dict) -> None:
    _svc_results.append((scenario, data))


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
    consecutive_passes: int = 1,
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
    consecutive_ok = 0
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
                consecutive_ok += 1
                if consecutive_ok >= consecutive_passes:
                    return data
            else:
                consecutive_ok = 0
            last_err = (
                f"status={data['status']} "
                f"active={len(active)}/{min_active} "
                f"bitrates=[{', '.join(str(round(v['smooth_bitrate_bps'] / 1000)) + 'kbps' for v in active)}]"
            )
        except Exception as exc:
            consecutive_ok = 0
            last_err = str(exc)
        time.sleep(2)
    raise TimeoutError(
        f"health check did not pass within {timeout}s — "
        f"needed {consecutive_passes} consecutive healthy polls, last: {last_err}"
    )


def poll_health_svc(
    url: str,
    timeout: int,
    min_active: int,
    min_sid: int = 2,
    min_tid: int = 1,
    min_bitrate_bps: int = 500_000,
) -> dict:
    """
    Poll GET url until SVC layer conditions are met:
      - status == "ok"
      - at least min_active active viewers
      - every active viewer has max_recv_sid >= min_sid and max_recv_tid >= min_tid
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
                and all(v.get("max_recv_sid", 0) >= min_sid for v in active)
                and all(v.get("max_recv_tid", 0) >= min_tid for v in active)
            ):
                return data
            svc_info = ", ".join(
                f"sid={v.get('max_recv_sid', '?')}/tid={v.get('max_recv_tid', '?')}"
                for v in active
            )
            last_err = (
                f"status={data['status']} "
                f"active={len(active)}/{min_active} "
                f"svc=[{svc_info}]"
            )
        except Exception as exc:
            last_err = str(exc)
        time.sleep(2)
    raise TimeoutError(f"SVC health check did not pass within {timeout}s -- last: {last_err}")


@contextlib.contextmanager
def rtcbench_run(
    config,
    video_dir: Path,
    network: str,
    recording_dir: Path = None,
    capture_dir: Path = None,
    env: dict = None,
    cap_add: list = None,
):
    """
    Start rtcbench in a Docker container joined to the given Docker network.

    config: str filename (resolved under ci/, e.g. "janus-smoke.yml")
            OR absolute Path to a YAML file (used for recording tests with overrides).
    video_dir: host directory mounted read-only as /test-videos inside the container.
    network: Docker network name to join (JANUS_NETWORK or JITSI_NETWORK).
    recording_dir: if given, mounted as /tmp/recordings (write).
    capture_dir: if given, mounted as /tmp/captures (write) for packet capture.
    env: if given, dict of environment variables passed to the container via -e flags.
    cap_add: if given, list of capabilities to add (e.g. ["NET_ADMIN"]).

    Yields (health_url, container_name, proc).
    On exit: stops the container, waits for the process, prints captured output.
    """
    name = f"rtcbench-e2e-{uuid.uuid4().hex[:8]}"
    config = Path(config)

    if config.is_absolute():
        ci_mounts = ["-v", f"{config}:/tmp/e2e-config.yml:ro"]
        container_config = "/tmp/e2e-config.yml"
    else:
        ci_mounts = ["-v", f"{REPO_ROOT / 'ci'}:/ci:ro"]
        container_config = f"/ci/{config}"

    env_flags = []
    if env:
        for k, v in env.items():
            env_flags += ["-e", f"{k}={v}"]

    cap_flags = []
    if cap_add:
        for cap in cap_add:
            cap_flags += ["--cap-add", cap]

    cmd = [
        "docker", "run", "--rm", "--name", name,
        "--network", network,
        "-p", "9090",
        "-v", f"{video_dir}:/test-videos:ro",
        *env_flags,
        *cap_flags,
        *ci_mounts,
    ]
    if recording_dir is not None:
        cmd += ["-v", f"{recording_dir}:/tmp/recordings"]
    if capture_dir is not None:
        cmd += ["-v", f"{capture_dir}:/tmp/captures"]
    cmd += ["rtcbench:latest", container_config]

    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    proc.container_name = name  # attach container name for callers that need docker exec

    for _ in range(30):
        r = subprocess.run(
            ["docker", "port", name, "9090"],
            capture_output=True, text=True,
        )
        if r.returncode == 0 and r.stdout.strip():
            host_port = r.stdout.strip().split(":")[-1]
            break
        time.sleep(0.5)
    else:
        raise RuntimeError(f"container {name} did not publish port 9090 within 15s")

    try:
        yield f"http://{_HEALTH_HOST}:{host_port}/health", proc
    finally:
        subprocess.run(["docker", "stop", name], capture_output=True, timeout=15)
        out, _ = proc.communicate(timeout=15)
        if out:
            print(f"\n--- rtcbench output ({config.name}) ---\n{out}---")


def apply_bandwidth_limit(container_name: str, rate_kbit: int) -> None:
    """Apply a bandwidth limit using tc tbf (token bucket filter).

    Creates a realistic bandwidth bottleneck where excess packets overflow
    the buffer and get dropped, just like a congested router. The burst
    parameter scales with the rate to avoid underflow at higher speeds.

    Requires NET_ADMIN capability and iproute2 in the container.
    """
    # Remove any existing qdisc first (ignore errors if none exists)
    subprocess.run(
        ["docker", "exec", container_name, "tc", "qdisc", "del", "dev", "eth0", "root"],
        check=False, capture_output=True,
    )
    # burst = max(15kb, rate_bytes_per_sec / 100) to scale with rate
    burst_bytes = max(15_000, rate_kbit * 1000 // 8 // 100)
    burst_kb = max(1, burst_bytes // 1000)
    subprocess.run(
        ["docker", "exec", container_name, "tc", "qdisc", "add", "dev", "eth0",
         "root", "handle", "1:", "tbf",
         "rate", f"{rate_kbit}kbit",
         "burst", f"{burst_kb}kb",
         "latency", "50ms"],
        check=True,
    )
    # Add 20ms +/- 5ms delay as child to simulate real network path
    subprocess.run(
        ["docker", "exec", container_name, "tc", "qdisc", "add", "dev", "eth0",
         "parent", "1:1", "handle", "10:", "netem",
         "delay", "20ms", "5ms"],
        check=True,
    )


def poll_health_snapshot(url: str, timeout: int) -> dict | None:
    """Poll health until we get a response with at least one active viewer.
    Returns the health payload, or None if it times out.
    """
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            r = requests.get(url, timeout=2)
            data = r.json()
            active = [v for v in data["viewers"] if v["last_seen_ago_ms"] <= 30_000]
            if data["status"] == "ok" and active:
                return data
        except Exception:
            pass
        time.sleep(2)
    return None


def remove_bandwidth_limit(container_name: str) -> None:
    """Remove any tc qdisc on the container."""
    subprocess.run(
        ["docker", "exec", container_name, "tc", "qdisc", "del", "dev", "eth0", "root"],
        check=False, capture_output=True,
    )


def build_pcap_config(base_config_name: str) -> Path:
    """
    Write a temporary YAML config identical to ci/<base_config_name> but with
    packetCapture.enabled=true and packetCapture.directory=/tmp/captures.

    Returns the path to the temp file. Caller is responsible for unlinking it.
    """
    with open(REPO_ROOT / "ci" / base_config_name) as f:
        cfg = yaml.safe_load(f)
    cfg["spec"]["conference"]["packetCapture"] = {
        "enabled": True,
        "directory": "/tmp/captures",
    }
    tmp = tempfile.NamedTemporaryFile(
        suffix=".yml", delete=False, mode="w", prefix="rtcbench-e2e-pcap-", dir="/tmp"
    )
    yaml.dump(cfg, tmp)
    tmp.close()
    return Path(tmp.name)


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
        suffix=".yml", delete=False, mode="w", prefix="rtcbench-e2e-", dir="/tmp"
    )
    yaml.dump(cfg, tmp)
    tmp.close()
    return Path(tmp.name)
