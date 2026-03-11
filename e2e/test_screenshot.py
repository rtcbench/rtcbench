"""
Screenshot smoke test — 2 senders, 2 receivers, then join in Chromium.

Starts call.zip with 2 senders and 2 receivers, waits for all receivers to
reach healthy bitrate, then opens Chromium (via Playwright) to join the
conference room and takes screenshots every 5 seconds for 15 seconds.

Works for Janus, Jitsi, and LiveKit.
"""
import time

import jwt
import pytest
from playwright.sync_api import sync_playwright

from helpers import (
    JANUS_NETWORK,
    JITSI_NETWORK,
    LIVEKIT_NETWORK,
    callzip_run,
    poll_health,
)

# 2 senders × 2 receivers = 4 active viewer entries in /health
MIN_ACTIVE = 4
SCREENSHOT_INTERVAL = 5
SCREENSHOT_COUNT = 4  # at t=0, 5, 10, 15 s

# ---------------------------------------------------------------------------
# Browser HTML pages for Janus and LiveKit (Jitsi has its own web UI)
# ---------------------------------------------------------------------------

_JANUS_HTML = """<!DOCTYPE html>
<html><head><title>Janus Viewer</title></head>
<body>
<div id="videos" style="display:flex;flex-wrap:wrap;gap:8px;background:#222;
     min-height:100vh;align-items:center;justify-content:center;"></div>
<script>
const SERVER = "JANUS_SERVER_URL";
const ROOM_ID = JANUS_ROOM_ID;

async function post(path, body) {
    const r = await fetch(SERVER + path, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({...body, transaction: "t" + Math.random().toString(36).slice(2)}),
    });
    return r.json();
}

async function main() {
    // Create session
    const sess = await post("", {janus: "create"});
    const sid = sess.data.id;

    // Attach to videoroom plugin
    const att = await post("/" + sid, {janus: "attach", plugin: "janus.plugin.videoroom"});
    const hid = att.data.id;

    // Long-poll for async plugin events (join, start).
    const events = {};
    (async function poll() {
        while (true) {
            try {
                const r = await fetch(SERVER + "/" + sid + "?maxev=1");
                const d = await r.json();
                if (d.transaction && events[d.transaction]) {
                    events[d.transaction](d);
                    delete events[d.transaction];
                }
            } catch(e) { break; }
        }
    })();

    // Send a synchronous plugin message and return the HTTP response directly.
    async function pluginMsg(body) {
        const txn = "t" + Math.random().toString(36).slice(2);
        const r = await fetch(SERVER + "/" + sid + "/" + hid, {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({janus: "message", body, transaction: txn}),
        });
        return r.json();
    }

    // Send an async plugin message and wait for the event via long-poll.
    function asyncMsg(body, jsep) {
        const txn = "t" + Math.random().toString(36).slice(2);
        return new Promise(resolve => {
            events[txn] = resolve;
            const msg = {janus: "message", body, transaction: txn};
            if (jsep) msg.jsep = jsep;
            fetch(SERVER + "/" + sid + "/" + hid, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(msg),
            });
        });
    }

    // List publishers (synchronous — result in HTTP response).
    // Filter to participants with published streams — call.zip viewers join as
    // "publisher" for discovery but never publish, so we must skip them.
    const list = await pluginMsg({request: "listparticipants", room: ROOM_ID});
    const all = list.plugindata.data.participants || [];
    var pubs = all.filter(p => p.streams && p.streams.length > 0);
    if (pubs.length === 0) pubs = all.filter(p => !p.display || !p.display.includes("-discovery"));
    if (pubs.length === 0) { document.title = "ERROR: no publishers"; return; }
    const streams = pubs.map(p => ({feed: p.id}));

    // Subscribe (async — JSEP offer arrives via long-poll)
    const joined = await asyncMsg({
        request: "join", ptype: "subscriber", room: ROOM_ID, streams: streams,
    });
    // WebRTC answer
    const pc = new RTCPeerConnection();
    pc.ontrack = (ev) => {
        if (ev.track.kind === "video") {
            const v = document.createElement("video");
            v.autoplay = true; v.muted = true; v.playsInline = true;
            v.style.cssText = "width:480px;height:270px;background:#000;";
            v.srcObject = new MediaStream([ev.track]);
            document.getElementById("videos").appendChild(v);
        }
    };
    await pc.setRemoteDescription(new RTCSessionDescription(joined.jsep));
    const answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    await asyncMsg({request: "start"}, {type: "answer", sdp: answer.sdp});

    document.title = "JOINED";
}
main().catch(e => { console.error(e); document.title = "ERROR: " + e.message; });
</script>
</body></html>"""

_LIVEKIT_HTML = """<!DOCTYPE html>
<html><head><title>LiveKit Viewer</title></head>
<body>
<div id="videos" style="display:flex;flex-wrap:wrap;gap:8px;background:#222;
     min-height:100vh;align-items:center;justify-content:center;"></div>
<script src="https://cdn.jsdelivr.net/npm/livekit-client@2/dist/livekit-client.umd.min.js"></script>
<script>
(async () => {
    const WS_URL = "LIVEKIT_WS_URL";
    const TOKEN  = "LIVEKIT_TOKEN";

    const room = new LivekitClient.Room({adaptiveStream: false, dynacast: false});

    room.on(LivekitClient.RoomEvent.TrackSubscribed, (track, pub, participant) => {
        if (track.kind === "video") {
            const el = track.attach();
            el.style.cssText = "width:480px;height:270px;background:#000;";
            document.getElementById("videos").appendChild(el);
        }
    });

    await room.connect(WS_URL, TOKEN);
    document.title = "JOINED";
})().catch(e => { console.error(e); document.title = "ERROR: " + e.message; });
</script>
</body></html>"""


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _generate_livekit_token(room_name: str, identity: str = "browser-viewer") -> str:
    """Generate a LiveKit access token (JWT) for subscribing to a room."""
    claims = {
        "exp": int(time.time()) + 3600,
        "iss": "devkey",
        "sub": identity,
        "jti": identity,
        "video": {
            "roomJoin": True,
            "room": room_name,
            "canSubscribe": True,
            "canPublish": False,
        },
    }
    return jwt.encode(claims, "secret", algorithm="HS256")


def _launch_browser(pw):
    """Launch headless Chromium with permissive flags for testing."""
    return pw.chromium.launch(
        headless=True,
        args=[
            "--autoplay-policy=no-user-gesture-required",
            "--disable-web-security",
            "--use-fake-device-for-media-stream",
            "--use-fake-ui-for-media-stream",
            "--no-sandbox",
        ],
    )


def _join_jitsi(page, room_name: str):
    """Navigate to the Jitsi Meet web UI and wait for remote video."""
    url = (
        f"https://172.21.0.20/{room_name}"
        "#config.prejoinConfig.enabled=false"
        "&config.startWithAudioMuted=true"
        "&config.startWithVideoMuted=true"
        "&config.disableDeepLinking=true"
        "&config.p2p.enabled=false"
        "&config.requireDisplayName=false"
        "&config.notifications=[]"
        "&userInfo.displayName=%22e2e-viewer%22"
    )
    page.goto(url, wait_until="load", timeout=60_000)
    page.wait_for_selector("video", timeout=60_000)


def _join_janus(page, room_id: int):
    """Load a minimal Janus VideoRoom subscriber page and wait for video."""
    html = (
        _JANUS_HTML
        .replace("JANUS_SERVER_URL", "http://172.20.0.10:8088/janus")
        .replace("JANUS_ROOM_ID", str(room_id))
    )
    page.set_content(html, wait_until="load")
    page.wait_for_function("document.title === 'JOINED' || document.title.startsWith('ERROR')", timeout=30_000)
    title = page.title()
    assert not title.startswith("ERROR"), f"Janus join failed: {title}"
    page.wait_for_selector("video", timeout=30_000)


def _join_livekit(page, room_name: str):
    """Load a minimal LiveKit subscriber page and wait for video."""
    token = _generate_livekit_token(room_name)
    html = (
        _LIVEKIT_HTML
        .replace("LIVEKIT_WS_URL", "ws://172.22.0.10:7880")
        .replace("LIVEKIT_TOKEN", token)
    )
    page.set_content(html, wait_until="load", timeout=30_000)
    page.wait_for_function("document.title === 'JOINED' || document.title.startsWith('ERROR')", timeout=30_000)
    title = page.title()
    assert not title.startswith("ERROR"), f"LiveKit join failed: {title}"
    page.wait_for_selector("video", timeout=30_000)


def _take_screenshots(page, sfu_name: str, out_dir):
    """Take SCREENSHOT_COUNT screenshots at SCREENSHOT_INTERVAL second intervals."""
    paths = []
    for i in range(SCREENSHOT_COUNT):
        if i > 0:
            time.sleep(SCREENSHOT_INTERVAL)
        path = out_dir / f"{sfu_name}-screenshot-{i * SCREENSHOT_INTERVAL:02d}s.png"
        page.screenshot(path=path)
        paths.append(path)
        print(f"  screenshot: {path.name} ({path.stat().st_size:,} bytes)")
    return paths


def _run_screenshot_test(sfu_name, config, network, join_fn, tmp_path, test_video_dir):
    """
    Core screenshot smoke test logic shared across SFU backends.

    1. Start call.zip with 2s2v config
    2. Wait for all receivers to reach healthy bitrate
    3. Join in Chromium via join_fn
    4. Take screenshots every 5 s for 15 s
    """
    timeout = 300 if sfu_name == "jitsi" else 120

    with callzip_run(config, test_video_dir, network=network) as (url, _proc):
        # Wait for receivers to reach target bitrate before opening browser.
        try:
            poll_health(url, timeout, MIN_ACTIVE)
        except TimeoutError as e:
            pytest.fail(str(e))

        # Join in Chromium and take screenshots.
        with sync_playwright() as pw:
            browser = _launch_browser(pw)
            context = browser.new_context(
                ignore_https_errors=True,
                viewport={"width": 1280, "height": 720},
            )
            page = context.new_page()
            try:
                join_fn(page)

                screenshots = _take_screenshots(page, sfu_name, tmp_path)
            finally:
                context.close()
                browser.close()

    # Basic assertions: screenshots exist and are non-trivially sized.
    for s in screenshots:
        assert s.exists(), f"{s.name} was not created"
        assert s.stat().st_size > 1_000, f"{s.name} is suspiciously small ({s.stat().st_size} bytes)"


# ---------------------------------------------------------------------------
# Tests — one per SFU, same xdist groups as other tests
# ---------------------------------------------------------------------------

@pytest.mark.xdist_group("janus")
def test_janus_screenshot(janus_infra, test_video_dir, tmp_path):
    _run_screenshot_test(
        sfu_name="janus",
        config="janus-smoke-2s2v.yml",
        network=JANUS_NETWORK,
        join_fn=lambda page: _join_janus(page, room_id=1234),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
    )


@pytest.mark.xdist_group("jitsi")
def test_jitsi_screenshot(jitsi_infra, test_video_dir, tmp_path):
    _run_screenshot_test(
        sfu_name="jitsi",
        config="jitsi-smoke-2s2v.yml",
        network=JITSI_NETWORK,
        join_fn=lambda page: _join_jitsi(page, room_name="smoke-test-room"),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
    )


@pytest.mark.xdist_group("livekit")
def test_livekit_screenshot(livekit_infra, test_video_dir, tmp_path):
    _run_screenshot_test(
        sfu_name="livekit",
        config="livekit-smoke-2s2v.yml",
        network=LIVEKIT_NETWORK,
        join_fn=lambda page: _join_livekit(page, room_name="livekit-2s2v"),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
    )
