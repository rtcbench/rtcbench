"""
Stats buffer size e2e test.

Verifies that a non-default statsBufferSize (256 instead of 768) works
correctly with a lower-bitrate video (1 Mbps instead of 3.5 Mbps).
The different packet rate exercises the stats pipeline at a different
operating point than the standard delivery tests.
"""
import pytest

from helpers import (
    JANUS_NETWORK, JITSI_NETWORK, LIVEKIT_NETWORK, MEDIASOUP_NETWORK,
    PLUGIN_ENV, rtcbench_run, poll_health, record_result,
)


def _run_bufsize256(infra, test_video_dir_1mbps, network, env, label, timeout=120):
    with rtcbench_run("smoke-bufsize256.yml", test_video_dir_1mbps, network=network, env=env) as (url, _proc):
        try:
            result = poll_health(url, timeout=timeout, min_active=1, min_bitrate_bps=800_000)
        except TimeoutError as e:
            pytest.fail(str(e))

    record_result(label, result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= 1
    for v in active:
        assert v["smooth_bitrate_bps"] >= 800_000, (
            f"{v['nickname']}: bitrate {v['smooth_bitrate_bps']/1000:.0f}kbps < 800kbps"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"


@pytest.mark.xdist_group("janus")
def test_janus_buffer_size_256(janus_infra, test_video_dir_1mbps):
    _run_bufsize256(janus_infra, test_video_dir_1mbps, JANUS_NETWORK, PLUGIN_ENV["janus"], "janus-bufsize256")


@pytest.mark.xdist_group("jitsi")
def test_jitsi_buffer_size_256(jitsi_infra, test_video_dir_1mbps):
    _run_bufsize256(jitsi_infra, test_video_dir_1mbps, JITSI_NETWORK, PLUGIN_ENV["jitsi"], "jitsi-bufsize256", timeout=300)


@pytest.mark.xdist_group("livekit")
def test_livekit_buffer_size_256(livekit_infra, test_video_dir_1mbps):
    _run_bufsize256(livekit_infra, test_video_dir_1mbps, LIVEKIT_NETWORK, PLUGIN_ENV["livekit"], "livekit-bufsize256")


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_buffer_size_256(mediasoup_infra, test_video_dir_1mbps):
    _run_bufsize256(mediasoup_infra, test_video_dir_1mbps, MEDIASOUP_NETWORK, PLUGIN_ENV["mediasoup"], "mediasoup-bufsize256")
