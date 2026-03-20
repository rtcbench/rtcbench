"""
pytest fixtures for call.zip e2e tests.

Session-scoped infrastructure fixtures start Docker Compose stacks once and
tear them down at the end of the session. The callzip binary runs as a
per-test Docker container (see helpers.callzip_run).
"""
import subprocess

import pytest

from helpers import REPO_ROOT, JANUS_PROJECT, JITSI_PROJECT, LIVEKIT_PROJECT, MEDIASOUP_PROJECT, _compose, get_delivery_results


# ---------------------------------------------------------------------------
# Pre-flight check
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session", autouse=True)
def ensure_docker_images():
    """Fail fast with a helpful message if callzip:latest is not built yet."""
    r = subprocess.run(
        ["docker", "image", "inspect", "callzip:latest"], capture_output=True
    )
    if r.returncode != 0:
        pytest.fail(
            "callzip:latest not found. Build it first:\n"
            "  make e2e-janus   # builds callzip + janus images then runs tests\n"
            "  make e2e-jitsi   # builds callzip + jitsi images then runs tests\n"
            "  make e2e         # builds all images then runs all tests"
        )


# ---------------------------------------------------------------------------
# Infrastructure fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def janus_infra(ensure_docker_images):
    """Start the Janus stack and wait for it to be healthy."""
    subprocess.run(
        _compose("up", "-d", "--wait", project=JANUS_PROJECT, profiles=("janus",)),
        check=True,
        cwd=REPO_ROOT,
    )
    yield
    subprocess.run(
        _compose("down", "-v", project=JANUS_PROJECT, profiles=("janus",)),
        check=True,
        cwd=REPO_ROOT,
    )


@pytest.fixture(scope="session")
def jitsi_infra(ensure_docker_images):
    """Start the Jitsi stack (prosody, jicofo, jvb, web) and wait for healthy."""
    subprocess.run(
        _compose("up", "-d", "--wait", project=JITSI_PROJECT, profiles=("jitsi",)),
        check=True,
        cwd=REPO_ROOT,
    )
    yield
    subprocess.run(
        _compose("down", "-v", project=JITSI_PROJECT, profiles=("jitsi",)),
        check=True,
        cwd=REPO_ROOT,
    )


@pytest.fixture(scope="session")
def livekit_infra(ensure_docker_images):
    """Start the LiveKit stack and wait for it to be healthy."""
    subprocess.run(
        _compose("up", "-d", "--wait", project=LIVEKIT_PROJECT, profiles=("livekit",)),
        check=True,
        cwd=REPO_ROOT,
    )
    yield
    subprocess.run(
        _compose("down", "-v", project=LIVEKIT_PROJECT, profiles=("livekit",)),
        check=True,
        cwd=REPO_ROOT,
    )


@pytest.fixture(scope="session")
def mediasoup_infra(ensure_docker_images):
    """Start the mediasoup stack and wait for it to be healthy."""
    subprocess.run(
        _compose("up", "-d", "--wait", project=MEDIASOUP_PROJECT, profiles=("mediasoup",)),
        check=True,
        cwd=REPO_ROOT,
    )
    yield
    subprocess.run(
        _compose("down", "-v", project=MEDIASOUP_PROJECT, profiles=("mediasoup",)),
        check=True,
        cwd=REPO_ROOT,
    )


# ---------------------------------------------------------------------------
# Test video fixture
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def test_video_dir(tmp_path_factory, ensure_docker_images):
    """
    Generate a VP9/IVF test video using the callzip container's ffmpeg.
    Returns the host directory containing test.ivf.
    """
    d = tmp_path_factory.mktemp("test-videos")
    subprocess.run(
        [
            "docker", "run", "--rm",
            "--entrypoint", "ffmpeg",
            "-v", f"{d}:/output",
            "callzip:latest",
            "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=25",
            "-t", "10",
            "-c:v", "libvpx-vp9", "-b:v", "3.5M",
            "-minrate", "3M", "-maxrate", "4M",
            "-g", "25",  # keyframe every 1 s — essential for browser decoding
            "-deadline", "realtime",
            "-f", "ivf", "/output/test.ivf",
            "-y", "-loglevel", "warning",
        ],
        check=True,
    )
    assert (d / "test.ivf").exists(), "test video generation failed"
    return d


@pytest.fixture(scope="session")
def test_video_dir_1mbps(tmp_path_factory, ensure_docker_images):
    """
    Generate a 1 Mbps VP9/IVF test video for stats buffer size tests.
    Lower bitrate produces fewer packets/sec, exercising the stats pipeline
    at a different operating point than the standard 3.5 Mbps video.
    """
    d = tmp_path_factory.mktemp("test-videos-1mbps")
    subprocess.run(
        [
            "docker", "run", "--rm",
            "--entrypoint", "ffmpeg",
            "-v", f"{d}:/output",
            "callzip:latest",
            "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=25",
            "-t", "10",
            "-c:v", "libvpx-vp9", "-b:v", "1M",
            "-minrate", "800k", "-maxrate", "1.2M",
            "-g", "25",
            "-deadline", "realtime",
            "-f", "ivf", "/output/test.ivf",
            "-y", "-loglevel", "warning",
        ],
        check=True,
    )
    assert (d / "test.ivf").exists(), "1 Mbps test video generation failed"
    return d


# ---------------------------------------------------------------------------
# Terminal summary hook
# ---------------------------------------------------------------------------

def pytest_terminal_summary(terminalreporter, exitstatus, config):
    results = get_delivery_results()
    if not results:
        return
    terminalreporter.write_sep("=", "e2e delivery results")
    header = f"  {'scenario':<28} {'active':>6}  {'aggregate':>12}"
    terminalreporter.write_line(header)
    terminalreporter.write_line("  " + "-" * (len(header) - 2))
    for name, data in results:
        terminalreporter.write_line(
            f"  {name:<28} {data['viewers_active']:>6}  "
            f"{data['aggregate_bitrate_mbps']:>9.2f} Mbps"
        )
