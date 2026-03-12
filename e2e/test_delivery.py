"""
Video delivery e2e tests.

Each scenario starts call.zip against a live SFU stack, polls the /health
endpoint until all expected viewers are active with healthy bitrate and fps,
then asserts on the result.

Adding a new scenario: add one tuple to the relevant SCENARIOS list.
No Makefile changes needed — pytest discovers and names the test automatically.
"""
import pytest

from helpers import (
    JANUS_NETWORK, JITSI_NETWORK, LIVEKIT_NETWORK,
    PLUGIN_ENV, callzip_run, poll_health, record_result,
)

JANUS_SCENARIOS = [
    ("smoke.yml",      1, 120),
    ("smoke-1s3v.yml", 3, 120),
    ("smoke-2s3v.yml", 6, 120),
]

JITSI_SCENARIOS = [
    ("smoke.yml",      1, 300),
    ("smoke-1s3v.yml", 3, 300),
    ("smoke-2s3v.yml", 6, 300),
]

LIVEKIT_SCENARIOS = [
    ("smoke.yml",      1, 120),
    ("smoke-1s3v.yml", 3, 120),
    ("smoke-2s3v.yml", 6, 300),
]


@pytest.mark.xdist_group("janus")
@pytest.mark.parametrize(
    "config,min_active,timeout",
    JANUS_SCENARIOS,
    ids=[f"janus-{s[0].removesuffix('.yml')}" for s in JANUS_SCENARIOS],
)
def test_janus_delivery(janus_infra, test_video_dir, config, min_active, timeout):
    with callzip_run(config, test_video_dir, network=JANUS_NETWORK, env=PLUGIN_ENV["janus"]) as (url, _proc):
        try:
            result = poll_health(url, timeout, min_active)
        except TimeoutError as e:
            pytest.fail(str(e))

    record_result(f"janus-{config.removesuffix('.yml')}", result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= min_active
    for v in active:
        assert v["smooth_bitrate_bps"] >= 3_000_000, (
            f"{v['nickname']}: bitrate {v['smooth_bitrate_bps']/1000:.0f}kbps < 3000kbps"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"


@pytest.mark.xdist_group("jitsi")
@pytest.mark.parametrize(
    "config,min_active,timeout",
    JITSI_SCENARIOS,
    ids=[f"jitsi-{s[0].removesuffix('.yml')}" for s in JITSI_SCENARIOS],
)
def test_jitsi_delivery(jitsi_infra, test_video_dir, config, min_active, timeout):
    with callzip_run(config, test_video_dir, network=JITSI_NETWORK, env=PLUGIN_ENV["jitsi"]) as (url, _proc):
        try:
            result = poll_health(url, timeout, min_active)
        except TimeoutError as e:
            pytest.fail(str(e))

    record_result(f"jitsi-{config.removesuffix('.yml')}", result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= min_active
    for v in active:
        assert v["smooth_bitrate_bps"] >= 3_000_000, (
            f"{v['nickname']}: bitrate {v['smooth_bitrate_bps']/1000:.0f}kbps < 3000kbps"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"


@pytest.mark.xdist_group("livekit")
@pytest.mark.parametrize(
    "config,min_active,timeout",
    LIVEKIT_SCENARIOS,
    ids=[f"livekit-{s[0].removesuffix('.yml')}" for s in LIVEKIT_SCENARIOS],
)
def test_livekit_delivery(livekit_infra, test_video_dir, config, min_active, timeout):
    with callzip_run(config, test_video_dir, network=LIVEKIT_NETWORK, env=PLUGIN_ENV["livekit"]) as (url, _proc):
        try:
            result = poll_health(url, timeout, min_active)
        except TimeoutError as e:
            pytest.fail(str(e))

    record_result(f"livekit-{config.removesuffix('.yml')}", result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= min_active
    for v in active:
        assert v["smooth_bitrate_bps"] >= 3_000_000, (
            f"{v['nickname']}: bitrate {v['smooth_bitrate_bps']/1000:.0f}kbps < 3000kbps"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"
