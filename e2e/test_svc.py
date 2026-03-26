"""
SVC (Scalable Video Coding) e2e tests.

Starts call.zip with a 3-layer SVC IVF file, polls /health, and asserts that
max_recv_tid reflects multi-layer temporal reception. Spatial layers depend on
SFU forwarding decisions (typically one per subscriber), so only temporal
layers are asserted end-to-end.

Add a new plugin by adding a tuple to the relevant SCENARIOS list and a test
function that takes the plugin's infra fixture.
"""
import time

import pytest

from helpers import (
    JANUS_NETWORK,
    JITSI_NETWORK,
    LIVEKIT_NETWORK,
    MEDIASOUP_NETWORK,
    PLUGIN_ENV,
    apply_bandwidth_limit,
    callzip_run,
    poll_health_snapshot,
    poll_health_svc,
    record_svc_result,
    remove_bandwidth_limit,
)

# (config, min_active, timeout, min_sid, min_tid)
# min_sid=0: spatial layer depends on SFU forwarding decision.
# min_tid=2: all 3 temporal layers (0, 1, 2) should be observed.
LIVEKIT_SVC_SCENARIOS = [
    ("smoke.yml", 1, 180, 0, 2),
]
JITSI_SVC_SCENARIOS = [
    ("smoke.yml", 1, 180, 0, 2),
]
JANUS_SVC_SCENARIOS = [
    ("smoke.yml", 1, 180, 0, 2),
]
MEDIASOUP_SVC_SCENARIOS = [
    ("smoke.yml", 1, 180, 0, 2),
]


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------


def _active_viewers(data: dict) -> list:
    return [v for v in data["viewers"] if v["last_seen_ago_ms"] <= 30_000]


def _avg_bitrate(data: dict) -> float:
    active = _active_viewers(data)
    if not active:
        return 0
    return sum(v["smooth_bitrate_bps"] for v in active) / len(active)


def _max_tid(data: dict) -> int:
    active = _active_viewers(data)
    if not active:
        return -1
    return max(v.get("max_recv_tid", 0) for v in active)


def _run_svc_delivery(test_svc_video_dir, network, env, config, min_active, timeout, min_sid, min_tid, label):
    with callzip_run(
        config,
        test_svc_video_dir,
        network=network,
        env=env,
    ) as (url, _proc):
        try:
            result = poll_health_svc(
                url, timeout, min_active, min_sid=min_sid, min_tid=min_tid,
                min_bitrate_bps=50_000,
            )
        except TimeoutError as e:
            pytest.fail(str(e))

    record_svc_result(label, result)

    active = [v for v in result["viewers"] if v["last_seen_ago_ms"] <= 30_000]
    assert result["viewers_active"] >= min_active
    for v in active:
        assert v.get("max_recv_sid", 0) >= min_sid, (
            f"{v['nickname']}: max_recv_sid={v.get('max_recv_sid', 0)} < {min_sid}"
        )
        assert v.get("max_recv_tid", 0) >= min_tid, (
            f"{v['nickname']}: max_recv_tid={v.get('max_recv_tid', 0)} < {min_tid}"
        )
        assert v["smooth_fps"] > 0, f"{v['nickname']}: fps is 0"


# (rate_kbit, description)
BANDWIDTH_STEPS = [
    (5000,  "5.0 Mbps - generous headroom"),
    (2500,  "2.5 Mbps - moderate constraint"),
    (1500,  "1.5 Mbps - significant constraint"),
    (800,   "0.8 Mbps - severe constraint"),
]


def _run_svc_bandwidth_degradation(test_svc_video_dir, network, env):
    """Step through tc tbf bandwidth constraints and verify the sender adapts."""
    with callzip_run(
        "smoke.yml",
        test_svc_video_dir,
        network=network,
        env=env,
        cap_add=["NET_ADMIN"],
    ) as (url, proc):
        container = proc.container_name

        try:
            baseline = poll_health_svc(url, 60, 1, min_sid=0, min_tid=0, min_bitrate_bps=100_000)
        except TimeoutError as e:
            pytest.fail(f"baseline failed: {e}")

        baseline_bps = _avg_bitrate(baseline)
        baseline_tid = _max_tid(baseline)
        print(f"\n  baseline: bitrate={baseline_bps/1e6:.2f} Mbps, max_tid={baseline_tid}")

        results = [("baseline", baseline_bps, baseline_tid)]

        for rate_kbit, desc in BANDWIDTH_STEPS:
            print(f"  applying tc tbf: {desc}")
            apply_bandwidth_limit(container, rate_kbit)

            time.sleep(15)  # GCC convergence

            snap = poll_health_snapshot(url, 30)
            if snap is None:
                print(f"    no health data at {rate_kbit}kbit")
                results.append((desc, 0, -1))
                continue

            bps = _avg_bitrate(snap)
            tid = _max_tid(snap)
            print(f"    result: bitrate={bps/1e6:.2f} Mbps, max_tid={tid}")
            results.append((desc, bps, tid))

        remove_bandwidth_limit(container)

    final_bps = results[-1][1]
    assert final_bps < baseline_bps, (
        f"expected degradation: baseline={baseline_bps/1e6:.2f} Mbps, "
        f"final={final_bps/1e6:.2f} Mbps"
    )

    if len(results) >= 3:
        first_constrained_bps = results[1][1]
        last_constrained_bps = results[-1][1]
        if first_constrained_bps > 0 and last_constrained_bps > 0:
            assert last_constrained_bps < first_constrained_bps, (
                f"expected progressive degradation: "
                f"at 5Mbps={first_constrained_bps/1e6:.2f}, "
                f"at 0.8Mbps={last_constrained_bps/1e6:.2f}"
            )

    print("\n  --- SVC bandwidth degradation summary ---")
    print(f"  {'step':<35} {'bitrate':>12} {'max_tid':>8}")
    print(f"  {'-'*55}")
    for desc, bps, tid in results:
        print(f"  {desc:<35} {bps/1e6:>9.2f} Mbps {tid:>8}")


# ---------------------------------------------------------------------------
# LiveKit
# ---------------------------------------------------------------------------


@pytest.mark.xdist_group("livekit")
@pytest.mark.parametrize(
    "config,min_active,timeout,min_sid,min_tid",
    LIVEKIT_SVC_SCENARIOS,
    ids=[f"livekit-svc-{s[0].removesuffix('.yml')}" for s in LIVEKIT_SVC_SCENARIOS],
)
def test_livekit_svc_delivery(
    livekit_infra, test_svc_video_dir, config, min_active, timeout, min_sid, min_tid
):
    _run_svc_delivery(
        test_svc_video_dir, LIVEKIT_NETWORK, PLUGIN_ENV["livekit"],
        config, min_active, timeout, min_sid, min_tid,
        f"livekit-svc-{config.removesuffix('.yml')}",
    )


@pytest.mark.xdist_group("livekit")
def test_livekit_svc_bandwidth_degradation(livekit_infra, test_svc_video_dir):
    _run_svc_bandwidth_degradation(test_svc_video_dir, LIVEKIT_NETWORK, PLUGIN_ENV["livekit"])


# ---------------------------------------------------------------------------
# Jitsi
# ---------------------------------------------------------------------------


@pytest.mark.xdist_group("jitsi")
@pytest.mark.parametrize(
    "config,min_active,timeout,min_sid,min_tid",
    JITSI_SVC_SCENARIOS,
    ids=[f"jitsi-svc-{s[0].removesuffix('.yml')}" for s in JITSI_SVC_SCENARIOS],
)
def test_jitsi_svc_delivery(
    jitsi_infra, test_svc_video_dir, config, min_active, timeout, min_sid, min_tid
):
    _run_svc_delivery(
        test_svc_video_dir, JITSI_NETWORK, PLUGIN_ENV["jitsi"],
        config, min_active, timeout, min_sid, min_tid,
        f"jitsi-svc-{config.removesuffix('.yml')}",
    )


@pytest.mark.xdist_group("jitsi")
def test_jitsi_svc_bandwidth_degradation(jitsi_infra, test_svc_video_dir):
    _run_svc_bandwidth_degradation(test_svc_video_dir, JITSI_NETWORK, PLUGIN_ENV["jitsi"])


# ---------------------------------------------------------------------------
# Janus
# ---------------------------------------------------------------------------


@pytest.mark.xdist_group("janus")
@pytest.mark.parametrize(
    "config,min_active,timeout,min_sid,min_tid",
    JANUS_SVC_SCENARIOS,
    ids=[f"janus-svc-{s[0].removesuffix('.yml')}" for s in JANUS_SVC_SCENARIOS],
)
def test_janus_svc_delivery(
    janus_infra, test_svc_video_dir, config, min_active, timeout, min_sid, min_tid
):
    _run_svc_delivery(
        test_svc_video_dir, JANUS_NETWORK, PLUGIN_ENV["janus"],
        config, min_active, timeout, min_sid, min_tid,
        f"janus-svc-{config.removesuffix('.yml')}",
    )


@pytest.mark.xdist_group("janus")
def test_janus_svc_bandwidth_degradation(janus_infra, test_svc_video_dir):
    _run_svc_bandwidth_degradation(test_svc_video_dir, JANUS_NETWORK, PLUGIN_ENV["janus"])


# ---------------------------------------------------------------------------
# Mediasoup
# ---------------------------------------------------------------------------


@pytest.mark.xdist_group("mediasoup")
@pytest.mark.parametrize(
    "config,min_active,timeout,min_sid,min_tid",
    MEDIASOUP_SVC_SCENARIOS,
    ids=[f"mediasoup-svc-{s[0].removesuffix('.yml')}" for s in MEDIASOUP_SVC_SCENARIOS],
)
def test_mediasoup_svc_delivery(
    mediasoup_infra, test_svc_video_dir, config, min_active, timeout, min_sid, min_tid
):
    _run_svc_delivery(
        test_svc_video_dir, MEDIASOUP_NETWORK, PLUGIN_ENV["mediasoup"],
        config, min_active, timeout, min_sid, min_tid,
        f"mediasoup-svc-{config.removesuffix('.yml')}",
    )


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_svc_bandwidth_degradation(mediasoup_infra, test_svc_video_dir):
    _run_svc_bandwidth_degradation(test_svc_video_dir, MEDIASOUP_NETWORK, PLUGIN_ENV["mediasoup"])
