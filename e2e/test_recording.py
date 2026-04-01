"""
Recording e2e tests.

Verifies that rtcbench writes valid VP9/IVF files when recording is enabled,
and that the recorded content is faithful to the source video.

For each recorded IVF file the test:
  1. Asserts non-trivial file size (catches empty/missing output).
  2. Runs ffprobe to confirm VP9 codec (catches wrong codec or corrupt container).
  3. Runs ffmpeg decode to confirm no bitstream errors (catches corrupt frames).
  4. Asserts frame fidelity against the source IVF (catches content mangling by the SFU).

Frame fidelity works as follows: the sender loops test.ivf continuously. The SFU
forwards VP9 RTP payloads unchanged (it may rewrite SSRCs, but not payloads), so
every frame in any recorded file is a bit-identical copy of a frame in the source.
We use ffmpeg's framemd5 muxer to get a hash per decoded frame, then find the
phase offset between the recording and the source loop, and assert that >= 95% of
recorded frames match the source at that offset. This correctly handles:
  - Multiple senders (all loop the same test.ivf)
  - Multiple receivers (each IVF file is checked independently)
  - SSRC rewriting by the SFU (we match by frame content, not SSRC)
  - Loop wrap-around (offset search is modulo source length)

ffprobe and ffmpeg run inside the rtcbench Docker image; no host-side ffmpeg needed.
"""
import json
import subprocess
import time

import pytest

from helpers import (
    JANUS_NETWORK, JITSI_NETWORK, LIVEKIT_NETWORK, MEDIASOUP_NETWORK,
    PLUGIN_ENV, rtcbench_run, poll_health, build_recording_config,
)


# ---------------------------------------------------------------------------
# Low-level helpers
# ---------------------------------------------------------------------------

def _docker_ffprobe(ivf_path) -> dict:
    out = subprocess.check_output(
        [
            "docker", "run", "--rm",
            "--entrypoint", "ffprobe",
            "-v", f"{ivf_path.parent}:/data:ro",
            "rtcbench:latest",
            "-v", "quiet",
            "-print_format", "json",
            "-show_streams",
            "-show_format",
            f"/data/{ivf_path.name}",
        ]
    )
    return json.loads(out)


def _docker_ffmpeg_decode(ivf_path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [
            "docker", "run", "--rm",
            "--entrypoint", "ffmpeg",
            "-v", f"{ivf_path.parent}:/data:ro",
            "rtcbench:latest",
            "-i", f"/data/{ivf_path.name}",
            "-f", "null", "-",
        ],
        capture_output=True,
    )


def _frame_md5s(ivf_path) -> list[str]:
    """
    Return a list of per-frame content hashes for ivf_path.

    Uses ffmpeg's framemd5 muxer, which decodes each frame and hashes the raw
    pixel data. The hash is deterministic for a given VP9 bitstream, so two
    files containing the same VP9 frames produce the same hashes regardless of
    which SSRC the RTP stream used or how the container timestamps were written.
    """
    result = subprocess.run(
        [
            "docker", "run", "--rm",
            "--entrypoint", "ffmpeg",
            "-v", f"{ivf_path.parent}:/data:ro",
            "rtcbench:latest",
            "-i", f"/data/{ivf_path.name}",
            "-f", "framemd5", "-",
        ],
        capture_output=True,
        text=True,
        check=True,
    )
    hashes = []
    for line in result.stdout.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        # framemd5 CSV: stream, pts, pts_time, dts, dts_time, duration,
        #               duration_time, size, hash
        # The hash is always the last field.
        hashes.append(line.split(",")[-1].strip())
    return hashes


# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------

def _assert_container_integrity(ivf_path):
    """Size, codec, and decode-clean checks for one IVF file."""
    size = ivf_path.stat().st_size
    assert size > 10_000, (
        f"{ivf_path.name}: file too small ({size} bytes) — recording may be empty"
    )

    info = _docker_ffprobe(ivf_path)
    streams = info.get("streams", [])
    assert streams, f"{ivf_path.name}: ffprobe found no streams"
    codec = streams[0].get("codec_name", "")
    assert codec == "vp9", f"{ivf_path.name}: expected codec vp9, got {codec!r}"

    result = _docker_ffmpeg_decode(ivf_path)
    assert result.returncode == 0, (
        f"{ivf_path.name}: ffmpeg decode failed:\n{result.stderr.decode()}"
    )


def _assert_frame_fidelity(source_ivf, recorded_ivf, min_match_rate: float = 0.95):
    """
    Assert that the frames in recorded_ivf are faithful copies of frames in
    source_ivf, accounting for an unknown loop phase offset.

    Since the SFU forwards VP9 payloads byte-for-byte (only RTP headers / SSRCs
    may be rewritten), decoded pixels must be identical to the source. We use
    framemd5 hashes rather than SSIM so the check is fast, exact, and independent
    of the temporal offset introduced by the sender loop.

    Algorithm:
      1. Hash every frame in source_ivf  →  source_hashes[0..N-1]
      2. Hash every frame in recorded_ivf →  recorded_hashes[0..M-1]
      3. Find offset k such that source_hashes[k] == recorded_hashes[0].
         (The first recorded frame is always a keyframe, so it is present in the
         source verbatim. If the source loops, k is found modulo N.)
      4. Count how many recorded_hashes[i] == source_hashes[(k+i) % N].
      5. Assert count / M >= min_match_rate.
    """
    source_hashes = _frame_md5s(source_ivf)
    recorded_hashes = _frame_md5s(recorded_ivf)

    assert source_hashes, f"no frames decoded from source {source_ivf.name}"
    assert recorded_hashes, f"no frames decoded from {recorded_ivf.name}"

    # Build lookup: hash -> list of positions in source (handles duplicate hashes
    # in pathological cases, though testsrc2 frames are all distinct).
    source_index: dict[str, list[int]] = {}
    for i, h in enumerate(source_hashes):
        source_index.setdefault(h, []).append(i)

    first_hash = recorded_hashes[0]
    assert first_hash in source_index, (
        f"{recorded_ivf.name}: first recorded frame not found in source — "
        "VP9 payload was altered by the SFU (or wrong source video)"
    )

    # Use the earliest matching position as the phase offset.
    offset = source_index[first_hash][0]
    n = len(source_hashes)

    matched = sum(
        1
        for i, h in enumerate(recorded_hashes)
        if h == source_hashes[(offset + i) % n]
    )
    match_rate = matched / len(recorded_hashes)

    assert match_rate >= min_match_rate, (
        f"{recorded_ivf.name}: frame fidelity {match_rate:.1%} < "
        f"{min_match_rate:.0%} (offset={offset}, "
        f"matched={matched}/{len(recorded_hashes)}) — "
        "SFU may be dropping or mangling VP9 frames"
    )


def _assert_recordings(recording_dir, source_ivf):
    """Run all integrity and fidelity checks on every IVF file in recording_dir."""
    ivf_files = sorted(recording_dir.glob("**/*.ivf"))
    assert ivf_files, f"no IVF files written to {recording_dir}"

    for ivf in ivf_files:
        _assert_container_integrity(ivf)
        _assert_frame_fidelity(source_ivf, ivf)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

def _run_recording_test(infra, test_video_dir, tmp_path, network, env, timeout=120):
    """Run a recording test: start rtcbench with recording enabled, wait for
    health, accumulate 10 s of data, then validate the IVF files."""
    recording_dir = tmp_path / "recordings"
    recording_dir.mkdir()
    cfg = build_recording_config("smoke.yml")
    try:
        with rtcbench_run(cfg, test_video_dir, network=network, recording_dir=recording_dir, env=env) as (url, _proc):
            try:
                poll_health(url, timeout=timeout, min_active=1)
            except TimeoutError as e:
                pytest.fail(str(e))
            time.sleep(10)  # accumulate recording post-warmup
        _assert_recordings(recording_dir, test_video_dir / "test.ivf")
    finally:
        cfg.unlink(missing_ok=True)


@pytest.mark.xdist_group("janus")
def test_janus_recording(janus_infra, test_video_dir, tmp_path):
    _run_recording_test(janus_infra, test_video_dir, tmp_path, JANUS_NETWORK, PLUGIN_ENV["janus"])


@pytest.mark.xdist_group("jitsi")
def test_jitsi_recording(jitsi_infra, test_video_dir, tmp_path):
    _run_recording_test(jitsi_infra, test_video_dir, tmp_path, JITSI_NETWORK, PLUGIN_ENV["jitsi"], timeout=300)


@pytest.mark.xdist_group("livekit")
def test_livekit_recording(livekit_infra, test_video_dir, tmp_path):
    _run_recording_test(livekit_infra, test_video_dir, tmp_path, LIVEKIT_NETWORK, PLUGIN_ENV["livekit"])


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_recording(mediasoup_infra, test_video_dir, tmp_path):
    _run_recording_test(mediasoup_infra, test_video_dir, tmp_path, MEDIASOUP_NETWORK, PLUGIN_ENV["mediasoup"])
