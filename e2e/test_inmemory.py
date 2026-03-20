"""
In-memory camera e2e tests.

Verifies that cameras.inMemory=true delivers video identically to disk mode.
Uses the generic ci/smoke-2s2v.yml config with $env: vars for PLUGIN_ID,
SERVER_IP, and ENABLE_IN_MEMORY_CAMERA.
"""
import pytest

from helpers import (
    JANUS_NETWORK, JITSI_NETWORK, LIVEKIT_NETWORK, MEDIASOUP_NETWORK,
    PLUGIN_ENV, callzip_run, poll_health, record_result,
)

CONFIG = "smoke-2s2v.yml"


def _run_inmemory(plugin, network, min_active, timeout, test_video_dir):
    env = {**PLUGIN_ENV[plugin], "ENABLE_IN_MEMORY_CAMERA": "true"}
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
    _run_inmemory("janus", JANUS_NETWORK, 4, 120, test_video_dir)


@pytest.mark.xdist_group("jitsi")
def test_jitsi_inmemory(jitsi_infra, test_video_dir):
    _run_inmemory("jitsi", JITSI_NETWORK, 4, 300, test_video_dir)


@pytest.mark.xdist_group("livekit")
def test_livekit_inmemory(livekit_infra, test_video_dir):
    _run_inmemory("livekit", LIVEKIT_NETWORK, 4, 120, test_video_dir)


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_inmemory(mediasoup_infra, test_video_dir):
    _run_inmemory("mediasoup", MEDIASOUP_NETWORK, 4, 120, test_video_dir)
