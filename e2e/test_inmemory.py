"""
In-memory camera e2e tests.

Verifies that cameras.inMemory=true delivers video identically to disk mode.
Uses a single config template (ci/inmemory-smoke-2s2v.yml) with $env: vars
for PLUGIN_ID, SERVER_IP, and ENABLE_IN_MEMORY_CAMERA.
"""
import pytest

from helpers import (
    JANUS_NETWORK, JITSI_NETWORK, LIVEKIT_NETWORK,
    callzip_run, poll_health, record_result,
)

CONFIG = "inmemory-smoke-2s2v.yml"


def _run_inmemory(plugin, server_ip, network, min_active, timeout, test_video_dir):
    env = {
        "PLUGIN_ID": plugin,
        "SERVER_IP": server_ip,
        "ENABLE_IN_MEMORY_CAMERA": "true",
    }
    with callzip_run(CONFIG, test_video_dir, network=network, env=env) as (url, _proc):
        try:
            result = poll_health(url, timeout, min_active)
        except TimeoutError as e:
            pytest.fail(str(e))

    record_result(f"inmemory-{plugin}-2s2v", result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= min_active
    for v in active:
        assert v["smooth_bitrate_bps"] >= 3_000_000, (
            f"{v['nickname']}: bitrate {v['smooth_bitrate_bps']/1000:.0f}kbps < 3000kbps"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"


@pytest.mark.xdist_group("janus")
def test_janus_inmemory(janus_infra, test_video_dir):
    _run_inmemory("janus", "172.20.0.10", JANUS_NETWORK, 4, 120, test_video_dir)


@pytest.mark.xdist_group("jitsi")
def test_jitsi_inmemory(jitsi_infra, test_video_dir):
    _run_inmemory("jitsi", "172.21.0.20", JITSI_NETWORK, 4, 300, test_video_dir)


@pytest.mark.xdist_group("livekit")
def test_livekit_inmemory(livekit_infra, test_video_dir):
    _run_inmemory("livekit", "172.22.0.10", LIVEKIT_NETWORK, 4, 120, test_video_dir)
