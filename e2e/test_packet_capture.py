"""
Packet capture e2e tests.

Verifies that rtcbench writes valid pcap files when packetCapture is enabled.
Each viewer writes one pcap file per incoming track, named by the viewer's
track nickname (userID[SSRC]).

For each pcap file the test:
  1. Asserts file size is non-trivial (catches empty/missing output).
  2. Validates the pcap global header (magic number, version, link type).
  3. Validates at least one packet record header is present.

Scenarios:
  - 1s1v (smoke.yml): 1 sender + 1 viewer -> at least 1 pcap file
  - 3s2v (smoke-2s3v.yml): 2 senders + 3 viewers -> multiple pcap files
    (each viewer receives one track per sender)
"""
import struct
import time

import pytest

from helpers import (
    JANUS_NETWORK,
    LIVEKIT_NETWORK,
    PLUGIN_ENV,
    rtcbench_run,
    poll_health,
    build_pcap_config,
)

PCAP_MAGIC = 0xA1B2C3D4
PCAP_LINK_ETHERNET = 1


def _assert_valid_pcap(pcap_path):
    """Validate pcap global header and at least one packet."""
    data = pcap_path.read_bytes()
    assert len(data) >= 24, (
        f"{pcap_path.name}: too small ({len(data)} bytes) for pcap global header"
    )

    magic, major, minor = struct.unpack_from("<IHH", data, 0)
    assert magic == PCAP_MAGIC, (
        f"{pcap_path.name}: bad magic 0x{magic:08x}, expected 0x{PCAP_MAGIC:08x}"
    )
    assert major == 2 and minor == 4, (
        f"{pcap_path.name}: unexpected version {major}.{minor}"
    )

    link_type = struct.unpack_from("<I", data, 20)[0]
    assert link_type == PCAP_LINK_ETHERNET, (
        f"{pcap_path.name}: link type {link_type}, expected {PCAP_LINK_ETHERNET}"
    )

    # At least one packet record after the 24-byte global header.
    assert len(data) > 24 + 16, (
        f"{pcap_path.name}: no packet records found (size={len(data)})"
    )


def _assert_captures(capture_dir, min_files: int):
    """Run pcap validation on every .pcap file under capture_dir."""
    pcap_files = sorted(capture_dir.glob("**/*.pcap"))
    assert len(pcap_files) >= min_files, (
        f"expected >= {min_files} pcap files, found {len(pcap_files)} in {capture_dir}"
    )

    for pcap in pcap_files:
        _assert_valid_pcap(pcap)
        # Each file should have substantial data (at least a few KB of RTP packets).
        assert pcap.stat().st_size > 1000, (
            f"{pcap.name}: only {pcap.stat().st_size} bytes — capture may be empty"
        )


# ---------------------------------------------------------------------------
# 1 sender, 1 viewer (smoke.yml = 2 users, 1 camera)
# ---------------------------------------------------------------------------

@pytest.mark.xdist_group("janus")
def test_janus_packet_capture_1s1v(janus_infra, test_video_dir, tmp_path):
    capture_dir = tmp_path / "captures"
    capture_dir.mkdir()
    cfg = build_pcap_config("smoke.yml")
    try:
        with rtcbench_run(cfg, test_video_dir, network=JANUS_NETWORK, capture_dir=capture_dir, env=PLUGIN_ENV["janus"]) as (url, _proc):
            try:
                poll_health(url, timeout=120, min_active=1)
            except TimeoutError as e:
                pytest.fail(str(e))
            time.sleep(5)  # accumulate capture data
        _assert_captures(capture_dir, min_files=1)
    finally:
        cfg.unlink(missing_ok=True)


@pytest.mark.xdist_group("livekit")
def test_livekit_packet_capture_1s1v(livekit_infra, test_video_dir, tmp_path):
    capture_dir = tmp_path / "captures"
    capture_dir.mkdir()
    cfg = build_pcap_config("smoke.yml")
    try:
        with rtcbench_run(cfg, test_video_dir, network=LIVEKIT_NETWORK, capture_dir=capture_dir, env=PLUGIN_ENV["livekit"]) as (url, _proc):
            try:
                poll_health(url, timeout=120, min_active=1)
            except TimeoutError as e:
                pytest.fail(str(e))
            time.sleep(5)
        _assert_captures(capture_dir, min_files=1)
    finally:
        cfg.unlink(missing_ok=True)


# ---------------------------------------------------------------------------
# 3 senders, 2 viewers (smoke-2s3v.yml = 5 users, 2 cameras -> actually 2s3v)
# We use smoke-1s3v.yml for a simpler 1s3v case: 1 sender + 3 viewers = 3 pcap files
# ---------------------------------------------------------------------------

@pytest.mark.xdist_group("janus")
def test_janus_packet_capture_3s2v(janus_infra, test_video_dir, tmp_path):
    """smoke-2s3v: 2 senders, 3 viewers. Each viewer gets 2 tracks -> 6 pcap files."""
    capture_dir = tmp_path / "captures"
    capture_dir.mkdir()
    cfg = build_pcap_config("smoke-2s3v.yml")
    try:
        with rtcbench_run(cfg, test_video_dir, network=JANUS_NETWORK, capture_dir=capture_dir, env=PLUGIN_ENV["janus"]) as (url, _proc):
            try:
                poll_health(url, timeout=120, min_active=6)
            except TimeoutError as e:
                pytest.fail(str(e))
            time.sleep(5)
        # 3 viewers x 2 senders = 6 tracks = 6 pcap files
        _assert_captures(capture_dir, min_files=6)
    finally:
        cfg.unlink(missing_ok=True)


@pytest.mark.xdist_group("livekit")
def test_livekit_packet_capture_3s2v(livekit_infra, test_video_dir, tmp_path):
    """smoke-2s3v: 2 senders, 3 viewers. Each viewer gets 2 tracks -> 6 pcap files."""
    capture_dir = tmp_path / "captures"
    capture_dir.mkdir()
    cfg = build_pcap_config("smoke-2s3v.yml")
    try:
        with rtcbench_run(cfg, test_video_dir, network=LIVEKIT_NETWORK, capture_dir=capture_dir, env=PLUGIN_ENV["livekit"]) as (url, _proc):
            try:
                poll_health(url, timeout=120, min_active=6)
            except TimeoutError as e:
                pytest.fail(str(e))
            time.sleep(5)
        _assert_captures(capture_dir, min_files=6)
    finally:
        cfg.unlink(missing_ok=True)
