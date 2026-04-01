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
    JITSI_NETWORK,
    LIVEKIT_NETWORK,
    MEDIASOUP_NETWORK,
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


def _run_pcap_test(infra, test_video_dir, tmp_path, base_config, network, env, min_active, min_files, timeout=120):
    """Run a packet capture test: start rtcbench with pcap enabled, wait for
    health, accumulate 5 s of data, then validate the pcap files."""
    capture_dir = tmp_path / "captures"
    capture_dir.mkdir()
    cfg = build_pcap_config(base_config)
    try:
        with rtcbench_run(cfg, test_video_dir, network=network, capture_dir=capture_dir, env=env) as (url, _proc):
            try:
                poll_health(url, timeout=timeout, min_active=min_active)
            except TimeoutError as e:
                pytest.fail(str(e))
            time.sleep(5)  # accumulate capture data
        _assert_captures(capture_dir, min_files=min_files)
    finally:
        cfg.unlink(missing_ok=True)


# ---------------------------------------------------------------------------
# 1 sender, 1 viewer (smoke.yml = 2 users, 1 camera)
# ---------------------------------------------------------------------------

@pytest.mark.xdist_group("janus")
def test_janus_packet_capture_1s1v(janus_infra, test_video_dir, tmp_path):
    _run_pcap_test(janus_infra, test_video_dir, tmp_path, "smoke.yml", JANUS_NETWORK, PLUGIN_ENV["janus"], min_active=1, min_files=1)


@pytest.mark.xdist_group("jitsi")
def test_jitsi_packet_capture_1s1v(jitsi_infra, test_video_dir, tmp_path):
    _run_pcap_test(jitsi_infra, test_video_dir, tmp_path, "smoke.yml", JITSI_NETWORK, PLUGIN_ENV["jitsi"], min_active=1, min_files=1, timeout=300)


@pytest.mark.xdist_group("livekit")
def test_livekit_packet_capture_1s1v(livekit_infra, test_video_dir, tmp_path):
    _run_pcap_test(livekit_infra, test_video_dir, tmp_path, "smoke.yml", LIVEKIT_NETWORK, PLUGIN_ENV["livekit"], min_active=1, min_files=1)


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_packet_capture_1s1v(mediasoup_infra, test_video_dir, tmp_path):
    _run_pcap_test(mediasoup_infra, test_video_dir, tmp_path, "smoke.yml", MEDIASOUP_NETWORK, PLUGIN_ENV["mediasoup"], min_active=1, min_files=1)


# ---------------------------------------------------------------------------
# 2 senders, 3 viewers (smoke-2s3v.yml): each viewer gets 2 tracks -> 6 pcap files
# ---------------------------------------------------------------------------

@pytest.mark.xdist_group("janus")
def test_janus_packet_capture_3s2v(janus_infra, test_video_dir, tmp_path):
    _run_pcap_test(janus_infra, test_video_dir, tmp_path, "smoke-2s3v.yml", JANUS_NETWORK, PLUGIN_ENV["janus"], min_active=6, min_files=6)


@pytest.mark.xdist_group("jitsi")
def test_jitsi_packet_capture_3s2v(jitsi_infra, test_video_dir, tmp_path):
    _run_pcap_test(jitsi_infra, test_video_dir, tmp_path, "smoke-2s3v.yml", JITSI_NETWORK, PLUGIN_ENV["jitsi"], min_active=6, min_files=6, timeout=300)


@pytest.mark.xdist_group("livekit")
def test_livekit_packet_capture_3s2v(livekit_infra, test_video_dir, tmp_path):
    _run_pcap_test(livekit_infra, test_video_dir, tmp_path, "smoke-2s3v.yml", LIVEKIT_NETWORK, PLUGIN_ENV["livekit"], min_active=6, min_files=6)


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_packet_capture_3s2v(mediasoup_infra, test_video_dir, tmp_path):
    _run_pcap_test(mediasoup_infra, test_video_dir, tmp_path, "smoke-2s3v.yml", MEDIASOUP_NETWORK, PLUGIN_ENV["mediasoup"], min_active=6, min_files=6)
