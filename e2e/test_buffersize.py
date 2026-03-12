"""
Stats buffer size e2e test.

Verifies that a non-default statsBufferSize (256 instead of 768) works
correctly with a lower-bitrate video (1 Mbps instead of 3.5 Mbps).
The different packet rate exercises the stats pipeline at a different
operating point than the standard delivery tests.
"""
import pytest

from helpers import LIVEKIT_NETWORK, PLUGIN_ENV, callzip_run, poll_health, record_result


@pytest.mark.xdist_group("livekit")
def test_livekit_buffer_size_256(livekit_infra, test_video_dir_1mbps):
    with callzip_run("smoke-bufsize256.yml", test_video_dir_1mbps, network=LIVEKIT_NETWORK, env=PLUGIN_ENV["livekit"]) as (url, _proc):
        try:
            result = poll_health(url, timeout=120, min_active=1, min_bitrate_bps=800_000)
        except TimeoutError as e:
            pytest.fail(str(e))

    record_result("livekit-bufsize256", result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= 1
    for v in active:
        assert v["smooth_bitrate_bps"] >= 800_000, (
            f"{v['nickname']}: bitrate {v['smooth_bitrate_bps']/1000:.0f}kbps < 800kbps"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"
