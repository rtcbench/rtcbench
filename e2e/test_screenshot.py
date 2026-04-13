"""
Screenshot smoke test — 2 senders, 2 receivers, then join in Chromium.

Starts rtcbench with 2 senders and 2 receivers, waits for all receivers to
reach healthy bitrate, then opens Chromium (via Playwright) to join the
conference room and takes screenshots every 5 seconds for 15 seconds.

Works for Janus, Jitsi, LiveKit, and Mediasoup.
"""
import time

import jwt
import pytest
import requests
from playwright.sync_api import sync_playwright

from helpers import (
    JANUS_NETWORK,
    JITSI_NETWORK,
    LIVEKIT_NETWORK,
    MEDIASOUP_NETWORK,
    PLUGIN_ENV,
    rtcbench_run,
    poll_health,
)

# 2 senders × 2 receivers = 4 active viewer entries in /health
MIN_ACTIVE = 4
SCREENSHOT_TIMES = [0, 5, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25]  # seconds after join

# ---------------------------------------------------------------------------
# Browser HTML pages for Janus and LiveKit (Jitsi has its own web UI)
# ---------------------------------------------------------------------------

_JANUS_HTML = """<!DOCTYPE html>
<html><head><title>Janus Viewer</title></head>
<body style="margin:0;background:#222;">
<div style="background:#1565C0;color:#fff;font:bold 20px sans-serif;padding:8px 16px;">janus</div>
<div id="grid" style="display:grid;grid-template-columns:repeat(2,1fr);gap:8px;
     padding:8px;min-height:calc(100vh - 40px);box-sizing:border-box;"></div>
<script>
const SERVER = "JANUS_SERVER_URL";
const ROOM_ID = JANUS_ROOM_ID;
const COLORS = ["#E57373","#64B5F6","#81C784","#FFD54F","#BA68C8","#4DD0E1","#FF8A65","#A1887F"];
function dbg(msg) { console.log(msg); }

function hc(s) { let h=0; for(let i=0;i<s.length;i++) h=(h*31+s.charCodeAt(i))|0; return h; }

function makeTile(label) {
    const t = document.createElement("div");
    t.style.cssText = "position:relative;background:#1a1a1a;border-radius:8px;overflow:hidden;" +
        "aspect-ratio:16/9;display:flex;align-items:center;justify-content:center;";
    const ph = document.createElement("div");
    ph.className = "ph";
    const c = document.createElement("div");
    const col = COLORS[Math.abs(hc(label)) % COLORS.length];
    c.style.cssText = "width:64px;height:64px;border-radius:50%;background:" + col +
        ";display:flex;align-items:center;justify-content:center;font-size:22px;" +
        "color:#fff;font-family:sans-serif;font-weight:bold;";
    c.textContent = label.substring(0, 2).toUpperCase();
    ph.appendChild(c);
    t.appendChild(ph);
    const b = document.createElement("div");
    b.style.cssText = "position:absolute;bottom:4px;left:4px;background:rgba(0,0,0,.6);" +
        "color:#fff;padding:2px 6px;border-radius:4px;font:11px sans-serif;" +
        "max-width:90%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
    b.textContent = label;
    t.appendChild(b);
    return t;
}

async function post(path, body) {
    const r = await fetch(SERVER + path, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({...body, transaction: "t" + Math.random().toString(36).slice(2)}),
    });
    return r.json();
}

async function main() {
    const sess = await post("", {janus: "create"});
    const sid = sess.data.id;

    const att = await post("/" + sid, {janus: "attach", plugin: "janus.plugin.videoroom"});
    const hid = att.data.id;

    // Long-poll for async events (shared across all handles in this session).
    const events = {};
    const unsolicitedHandlers = [];  // handlers for events not matched to a txn
    (async function poll() {
        while (true) {
            try {
                const r = await fetch(SERVER + "/" + sid + "?maxev=5");
                const data = await r.json();
                for (const d of (Array.isArray(data) ? data : [data])) {
                    if (d.transaction && events[d.transaction]) {
                        events[d.transaction](d);
                        delete events[d.transaction];
                    } else {
                        for (const h of unsolicitedHandlers) h(d);
                    }
                }
            } catch(e) { await new Promise(r => setTimeout(r, 500)); }
        }
    })();

    function pluginMsg(handleId, body) {
        const txn = "t" + Math.random().toString(36).slice(2);
        return fetch(SERVER + "/" + sid + "/" + handleId, {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({janus: "message", body, transaction: txn}),
        }).then(r => r.json());
    }

    function asyncMsg(handleId, body, jsep) {
        const txn = "t" + Math.random().toString(36).slice(2);
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => {
                delete events[txn]; reject(new Error("async timeout"));
            }, 15000);
            events[txn] = d => { clearTimeout(timer); resolve(d); };
            const msg = {janus: "message", body, transaction: txn};
            if (jsep) msg.jsep = jsep;
            fetch(SERVER + "/" + sid + "/" + handleId, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(msg),
            });
        });
    }

    // Step 1: Join as publisher to get the active publishers list.
    // This mirrors the mvideoroom.html approach and triggers proper Janus
    // publisher-notification flow (including keyframe requests).
    const joined = await asyncMsg(hid, {
        request: "join", ptype: "publisher", room: ROOM_ID,
        display: "browser-viewer",
    });
    const pubData = joined.plugindata && joined.plugindata.data;
    if (!pubData || pubData.videoroom !== "joined") {
        document.title = "ERROR: publisher join failed"; return;
    }
    const publishers = pubData.publishers || [];
    dbg("publishers=" + publishers.length + " ids=[" + publishers.map(p=>p.id).join(",") + "]");

    // Step 2: Also get ALL participants (including non-publishing viewers)
    // via listparticipants so we can show tiles for everyone.
    const list = await pluginMsg(hid, {request: "listparticipants", room: ROOM_ID});
    const allParts = list.plugindata.data.participants || [];
    dbg("participants=" + allParts.length + " " + allParts.map(p=>(p.display||p.id)+"(pub="+!!p.publisher+")").join(", "));

    // Build tile grid for ALL participants.
    const grid = document.getElementById("grid");
    const tileMap = {};
    for (const p of allParts) {
        const raw = p.display || ("id-" + p.id);
        if (raw === "browser-viewer") continue;  // skip ourselves
        const label = raw.replace(/-discovery$/, "");
        const tile = makeTile(label);
        grid.appendChild(tile);
        tileMap[p.id] = tile;
    }

    // Step 3: Subscribe to all active publishers in a single multistream
    // PeerConnection via a second handle (the standard Janus pattern).
    if (publishers.length === 0) { document.title = "ERROR: no publishers"; return; }
    const streams = publishers.map(p => ({feed: p.id}));

    const subAtt = await post("/" + sid, {janus: "attach", plugin: "janus.plugin.videoroom"});
    const subHid = subAtt.data.id;

    const subJoined = await asyncMsg(subHid, {
        request: "join", ptype: "subscriber", room: ROOM_ID,
        streams: streams, private_id: pubData.private_id,
    });
    if (!subJoined.jsep) { document.title = "ERROR: no JSEP offer"; return; }

    // Map mids to feed IDs so we can place video in the correct tile.
    const midToFeed = {};
    const subStreams = subJoined.plugindata &&
        subJoined.plugindata.data && subJoined.plugindata.data.streams;
    if (subStreams) {
        for (const s of subStreams) {
            if (s.type === "video") midToFeed[s.mid] = s.feed_id;
        }
    }
    const pc = new RTCPeerConnection();
    pc.ontrack = (ev) => {
        if (ev.track.kind !== "video") return;
        // Determine which publisher this track belongs to via mid mapping.
        const mid = ev.transceiver && ev.transceiver.mid;
        const feedId = midToFeed[mid];
        dbg("ontrack video mid=" + mid + " feedId=" + feedId + " hasTile=" + !!tileMap[feedId]);
        const tile = tileMap[feedId] ||
            Object.values(tileMap).find(t => !t.querySelector("video"));
        if (!tile) return;

        const ph = tile.querySelector(".ph");
        if (ph) ph.style.display = "none";
        const v = document.createElement("video");
        v.autoplay = true; v.muted = true; v.playsInline = true;
        v.style.cssText = "position:absolute;top:0;left:0;width:100%;height:100%;" +
            "object-fit:cover;";
        v.srcObject = new MediaStream([ev.track]);
        tile.insertBefore(v, tile.firstChild);
        v.play().catch(() => {});
    };

    await pc.setRemoteDescription(new RTCSessionDescription(subJoined.jsep));
    const answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    await asyncMsg(subHid, {request: "start"}, {type: "answer", sdp: answer.sdp});
    dbg("subscribed feeds=" + publishers.length + " midToFeed=" + JSON.stringify(midToFeed));

    // Handle late-arriving publishers: subscribe dynamically and renegotiate.
    const subscribedFeeds = new Set(publishers.map(p => p.id));
    unsolicitedHandlers.push(async (ev) => {
        const pd = ev.plugindata && ev.plugindata.data;
        if (!pd || !pd.publishers) return;
        const newPubs = pd.publishers.filter(p => !subscribedFeeds.has(p.id));
        if (newPubs.length === 0) return;
        dbg("NEW pubs arrived: " + newPubs.length + " ids=[" + newPubs.map(p=>p.id).join(",") + "]");
        for (const p of newPubs) subscribedFeeds.add(p.id);

        // Create tiles for new publishers if needed.
        for (const p of newPubs) {
            if (!tileMap[p.id]) {
                const label = (p.display || ("id-" + p.id)).replace(/-discovery$/, "");
                const tile = makeTile(label);
                grid.appendChild(tile);
                tileMap[p.id] = tile;
            }
        }

        // Subscribe to new feeds on the existing subscriber handle.
        const subResp = await asyncMsg(subHid,
            {request: "subscribe", streams: newPubs.map(p => ({feed: p.id}))});
        if (!subResp.jsep) { console.error("no offer for new feeds"); return; }

        // Update mid→feed mapping from the updated response.
        const ns = subResp.plugindata && subResp.plugindata.data &&
            subResp.plugindata.data.streams;
        if (ns) {
            for (const s of ns) {
                if (s.type === "video") midToFeed[s.mid] = s.feed_id;
            }
        }

        // Renegotiate the PeerConnection.
        await pc.setRemoteDescription(new RTCSessionDescription(subResp.jsep));
        const ans = await pc.createAnswer();
        await pc.setLocalDescription(ans);
        await asyncMsg(subHid, {request: "start"}, {type: "answer", sdp: ans.sdp});
        dbg("subscribed to " + newPubs.length + " new feeds, midToFeed=" + JSON.stringify(midToFeed));
    });

    document.title = "JOINED";
}
main().catch(e => { console.error(e); document.title = "ERROR: " + e.message; });
</script>
</body></html>"""

_LIVEKIT_CLIENT_VERSION = "1.15.13"
_LIVEKIT_CLIENT_URL = f"https://cdn.jsdelivr.net/npm/livekit-client@{_LIVEKIT_CLIENT_VERSION}/dist/livekit-client.umd.min.js"

_LIVEKIT_HTML = """<!DOCTYPE html>
<html><head><title>LiveKit Viewer</title></head>
<body style="margin:0;background:#222;">
<div style="background:#E65100;color:#fff;font:bold 20px sans-serif;padding:8px 16px;">livekit</div>
<div id="grid" style="display:grid;grid-template-columns:repeat(2,1fr);gap:8px;
     padding:8px;min-height:calc(100vh - 40px);box-sizing:border-box;"></div>
<script src="LIVEKIT_CLIENT_URL"></script>
<script>
const WS_URL = "LIVEKIT_WS_URL";
const TOKEN  = "LIVEKIT_TOKEN";
const COLORS = ["#E57373","#64B5F6","#81C784","#FFD54F","#BA68C8","#4DD0E1","#FF8A65","#A1887F"];

function hc(s) { let h=0; for(let i=0;i<s.length;i++) h=(h*31+s.charCodeAt(i))|0; return h; }

function getOrCreateTile(identity) {
    let t = document.getElementById("t-" + identity);
    if (t) return t;
    t = document.createElement("div");
    t.id = "t-" + identity;
    t.style.cssText = "position:relative;background:#1a1a1a;border-radius:8px;overflow:hidden;" +
        "aspect-ratio:16/9;display:flex;align-items:center;justify-content:center;";
    const ph = document.createElement("div");
    ph.className = "ph";
    const c = document.createElement("div");
    const col = COLORS[Math.abs(hc(identity)) % COLORS.length];
    c.style.cssText = "width:64px;height:64px;border-radius:50%;background:" + col +
        ";display:flex;align-items:center;justify-content:center;font-size:22px;" +
        "color:#fff;font-family:sans-serif;font-weight:bold;";
    c.textContent = identity.substring(0, 2).toUpperCase();
    ph.appendChild(c);
    t.appendChild(ph);
    const b = document.createElement("div");
    b.style.cssText = "position:absolute;bottom:4px;left:4px;background:rgba(0,0,0,.6);" +
        "color:#fff;padding:2px 6px;border-radius:4px;font:11px sans-serif;" +
        "max-width:90%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
    b.textContent = identity;
    t.appendChild(b);
    document.getElementById("grid").appendChild(t);
    return t;
}

function attachVideo(tile, track) {
    if (!track) return;
    const ph = tile.querySelector(".ph");
    if (ph) ph.style.display = "none";
    const el = track.attach();
    el.style.cssText = "position:absolute;top:0;left:0;width:100%;height:100%;" +
        "object-fit:cover;";
    tile.insertBefore(el, tile.firstChild);
}

(async () => {
    const room = new LivekitClient.Room({adaptiveStream: false, dynacast: false});

    room.on(LivekitClient.RoomEvent.TrackSubscribed, (track, pub, participant) => {
        if (track.kind === "video") {
            const tile = getOrCreateTile(participant.identity);
            attachVideo(tile, track);
        }
    });

    room.on(LivekitClient.RoomEvent.ParticipantConnected, (p) => {
        getOrCreateTile(p.identity);
    });

    await room.connect(WS_URL, TOKEN);

    // Create tiles for all participants already in the room.
    const participants = room.remoteParticipants || room.participants || new Map();
    participants.forEach((p) => {
        const tile = getOrCreateTile(p.identity);
        const publications = p.videoTrackPublications || p.tracks || new Map();
        publications.forEach((pub) => {
            const track = pub && (pub.track || pub.videoTrack);
            const kind = pub && (pub.kind || (track && track.kind));
            if (kind === "video" && track && pub.isSubscribed !== false) {
                attachVideo(tile, track);
            }
        });
    });

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
    return jwt.encode(claims, "secret-secret-secret-secret-secret", algorithm="HS256")


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
    page.evaluate("""() => {
        const h = document.createElement('div');
        h.style.cssText = 'position:fixed;top:0;left:0;right:0;background:#2E7D32;color:#fff;' +
            'font:bold 20px sans-serif;padding:8px 16px;z-index:99999;';
        h.textContent = 'jitsi';
        document.body.prepend(h);
    }""")


def _join_janus(page, room_id: int):
    """Load a minimal Janus VideoRoom subscriber page and wait for video."""
    html = (
        _JANUS_HTML
        .replace("JANUS_SERVER_URL", "http://172.20.0.10:8088/janus")
        .replace("JANUS_ROOM_ID", str(room_id))
    )
    page.set_content(html, wait_until="load")
    # Longer timeout: publisher polling can take up to 15 s before subscribing.
    page.wait_for_function("document.title === 'JOINED' || document.title.startsWith('ERROR')", timeout=60_000)
    title = page.title()
    assert not title.startswith("ERROR"), f"Janus join failed: {title}"
    page.wait_for_selector("video", timeout=30_000)


def _join_livekit(page, room_name: str):
    """Load a minimal LiveKit subscriber page and wait for video."""
    _prefetch_livekit_client()

    token = _generate_livekit_token(room_name)
    html = (
        _LIVEKIT_HTML
        .replace("LIVEKIT_CLIENT_URL", _LIVEKIT_CLIENT_URL)
        .replace("LIVEKIT_WS_URL", "ws://172.22.0.10:7880")
        .replace("LIVEKIT_TOKEN", token)
    )
    # Serve the pre-fetched LiveKit UMD bundle so the browser never hits the CDN.
    def handle_cdn(route):
        url = route.request.url
        if url in _CDN_CACHE:
            route.fulfill(status=200, content_type="application/javascript; charset=utf-8", body=_CDN_CACHE[url])
        else:
            route.continue_()
    page.route("https://cdn.jsdelivr.net/**", handle_cdn)

    page.set_content(html, wait_until="load", timeout=60_000)
    page.wait_for_function("document.title === 'JOINED' || document.title.startsWith('ERROR')", timeout=60_000)
    title = page.title()
    assert not title.startswith("ERROR"), f"LiveKit join failed: {title}"
    page.wait_for_selector("video", timeout=30_000)


def _take_screenshots(page, sfu_name: str, out_dir):
    """Take screenshots at the times listed in SCREENSHOT_TIMES."""
    paths = []
    prev = 0
    for t in SCREENSHOT_TIMES:
        if t > prev:
            time.sleep(t - prev)
        prev = t
        path = out_dir / f"{sfu_name}-screenshot-{t:02d}s.png"
        page.screenshot(path=path)
        paths.append(path)
        print(f"  screenshot: {path.name} ({path.stat().st_size:,} bytes)")
    return paths


def _run_screenshot_test(sfu_name, config, network, join_fn, tmp_path, test_video_dir, env=None):
    """
    Core screenshot smoke test logic shared across SFU backends.

    1. Start rtcbench with 2s2v config
    2. Wait for all receivers to reach healthy bitrate
    3. Join in Chromium via join_fn
    4. Take screenshots every 5 s for 15 s
    """
    timeout = 300 if sfu_name == "jitsi" else 120

    with rtcbench_run(config, test_video_dir, network=network, env=env) as (url, _proc):
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
            page.on("console", lambda msg: print(f"  [browser] {msg.text}"))
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
        config="smoke-2s2v.yml",
        network=JANUS_NETWORK,
        join_fn=lambda page: _join_janus(page, room_id=1234),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
        env=PLUGIN_ENV["janus"],
    )


@pytest.mark.xdist_group("jitsi")
def test_jitsi_screenshot(jitsi_infra, test_video_dir, tmp_path):
    _run_screenshot_test(
        sfu_name="jitsi",
        config="smoke-2s2v.yml",
        network=JITSI_NETWORK,
        join_fn=lambda page: _join_jitsi(page, room_name="room-1234"),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
        env=PLUGIN_ENV["jitsi"],
    )


@pytest.mark.xdist_group("livekit")
def test_livekit_screenshot(livekit_infra, test_video_dir, tmp_path):
    _run_screenshot_test(
        sfu_name="livekit",
        config="smoke-2s2v.yml",
        network=LIVEKIT_NETWORK,
        join_fn=lambda page: _join_livekit(page, room_name="room-1234"),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
        env=PLUGIN_ENV["livekit"],
    )


_MEDIASOUP_HTML = """<!DOCTYPE html>
<html><head><title>Mediasoup Viewer</title></head>
<body style="margin:0;background:#222;">
<div style="background:#9C27B0;color:#fff;font:bold 20px sans-serif;padding:8px 16px;">mediasoup</div>
<div id="grid" style="display:grid;grid-template-columns:repeat(2,1fr);gap:8px;
     padding:8px;min-height:calc(100vh - 40px);box-sizing:border-box;"></div>
<script type="module">
import { Device } from 'https://esm.sh/mediasoup-client@3';

const WS_URL = "MEDIASOUP_WS_URL";
const ROOM_ID = "MEDIASOUP_ROOM_ID";
const COLORS = ["#E57373","#64B5F6","#81C784","#FFD54F","#BA68C8","#4DD0E1","#FF8A65","#A1887F"];
function hc(s) { let h=0; for(let i=0;i<s.length;i++) h=(h*31+s.charCodeAt(i))|0; return h; }

function getOrCreateTile(identity) {
    let t = document.getElementById("t-" + identity);
    if (t) return t;
    t = document.createElement("div");
    t.id = "t-" + identity;
    t.style.cssText = "position:relative;background:#1a1a1a;border-radius:8px;overflow:hidden;" +
        "aspect-ratio:16/9;display:flex;align-items:center;justify-content:center;";
    const ph = document.createElement("div");
    ph.className = "ph";
    const c = document.createElement("div");
    const col = COLORS[Math.abs(hc(identity)) % COLORS.length];
    c.style.cssText = "width:64px;height:64px;border-radius:50%;background:" + col +
        ";display:flex;align-items:center;justify-content:center;font-size:22px;" +
        "color:#fff;font-family:sans-serif;font-weight:bold;";
    c.textContent = identity.substring(0, 2).toUpperCase();
    ph.appendChild(c);
    t.appendChild(ph);
    const b = document.createElement("div");
    b.style.cssText = "position:absolute;bottom:4px;left:4px;background:rgba(0,0,0,.6);" +
        "color:#fff;padding:2px 6px;border-radius:4px;font:11px sans-serif;" +
        "max-width:90%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
    b.textContent = identity;
    t.appendChild(b);
    document.getElementById("grid").appendChild(t);
    return t;
}

// Minimal protoo-client over WebSocket.
class Protoo {
    constructor(url) {
        this._ws = new WebSocket(url, "protoo");
        this._pending = {};
        this._nextId = 1;
        this.onRequest = null;
        this._ws.onmessage = (e) => this._onMessage(JSON.parse(e.data));
    }
    _onMessage(msg) {
        if (msg.response) {
            const p = this._pending[msg.id];
            if (p) { delete this._pending[msg.id]; msg.ok ? p.resolve(msg.data) : p.reject(new Error(msg.errorReason)); }
        } else if (msg.request && this.onRequest) {
            this.onRequest(msg.method, msg.data).then(result => {
                this._ws.send(JSON.stringify({response:true, id:msg.id, ok:true, data:result||{}}));
            });
        } else if (msg.notification && this.onNotification) {
            this.onNotification(msg.method, msg.data);
        }
    }
    request(method, data) {
        return new Promise((resolve, reject) => {
            const id = this._nextId++;
            this._pending[id] = {resolve, reject};
            this._ws.send(JSON.stringify({request:true, id, method, data: data||{}}));
        });
    }
    notify(method, data) {
        this._ws.send(JSON.stringify({notification:true, method, data: data||{}}));
    }
    ready() {
        return new Promise((resolve, reject) => {
            if (this._ws.readyState === 1) resolve();
            else { this._ws.onopen = () => resolve(); this._ws.onerror = (e) => reject(e); }
        });
    }
}

async function main() {
    const peer = "browser-viewer-" + Math.random().toString(36).slice(2, 8);
    const protoo = new Protoo(WS_URL + "/?roomId=" + ROOM_ID + "&peerId=" + peer);
    await protoo.ready();
    console.log("protoo connected");

    const routerCaps = await protoo.request("getRouterRtpCapabilities");
    const caps = routerCaps.routerRtpCapabilities || routerCaps;

    const device = new Device();
    await device.load({routerRtpCapabilities: caps});
    console.log("device loaded, canProduce video=" + device.canProduce("video"));

    const rawTransport = await protoo.request("createWebRtcTransport", {
        forceTcp: false, appData: {direction: "consumer"},
    });
    // mediasoup-demo returns transportId; mediasoup-client expects id.
    const transportData = {...rawTransport, id: rawTransport.id || rawTransport.transportId};
    const recvTransport = device.createRecvTransport(transportData);

    recvTransport.on("connect", ({dtlsParameters}, callback, errback) => {
        protoo.request("connectWebRtcTransport", {
            transportId: recvTransport.id, dtlsParameters,
        }).then(callback).catch(errback);
    });
    console.log("recv transport created: " + recvTransport.id);

    protoo.onRequest = async (method, data) => {
        if (method !== "newConsumer") return {};
        console.log("newConsumer kind=" + data.kind + " peer=" + data.peerId);

        try {
            const consumer = await recvTransport.consume({
                id: data.id || data.consumerId,
                producerId: data.producerId,
                kind: data.kind,
                rtpParameters: data.rtpParameters,
            });
            console.log("consumed " + consumer.kind + " track=" + consumer.track.id);

            if (consumer.kind === "video") {
                const tile = getOrCreateTile(data.peerId || data.appData?.source || "unknown");
                const ph = tile.querySelector(".ph");
                if (ph) ph.style.display = "none";
                const v = document.createElement("video");
                v.autoplay = true; v.muted = true; v.playsInline = true;
                v.style.cssText = "position:absolute;top:0;left:0;width:100%;height:100%;object-fit:cover;";
                v.srcObject = new MediaStream([consumer.track]);
                tile.insertBefore(v, tile.firstChild);
                v.play().catch(() => {});
            }

            protoo.notify("resumeConsumer", {consumerId: data.id});
        } catch(e) {
            console.error("consume error: " + e.message);
        }
        return {};
    };

    protoo.onNotification = (method, data) => {
        if (method === "newPeer") {
            console.log("newPeer: " + data.id + " display=" + data.displayName);
            getOrCreateTile(data.id);
        }
    };

    const joinResult = await protoo.request("join", {
        displayName: peer,
        rtpCapabilities: device.rtpCapabilities,
        device: {name: "RTCBench-e2e", flag: "browser"},
    });
    console.log("joined room, peers=" + (joinResult.peers || []).length);
    for (const p of (joinResult.peers || [])) {
        getOrCreateTile(p.id || p.peerId);
    }
    document.title = "JOINED";
}
main().catch(e => { console.error(e); document.title = "ERROR: " + e.message; });
</script>
</body></html>"""


_CDN_CACHE: dict[str, str] = {}


def _prefetch_livekit_client():
    """Pre-fetch the LiveKit client UMD bundle from jsdelivr.

    A single file, but under parallel load the CDN can be slow enough to
    cause Chromium to give up, leaving LivekitClient undefined.
    """
    if _LIVEKIT_CLIENT_URL in _CDN_CACHE:
        return
    r = requests.get(_LIVEKIT_CLIENT_URL, timeout=30)
    r.raise_for_status()
    _CDN_CACHE[_LIVEKIT_CLIENT_URL] = r.text


def _prefetch_mediasoup_client():
    """Pre-fetch mediasoup-client and all its ESM sub-imports from esm.sh.

    The esm.sh CDN splits the package into many small modules (one per import).
    Under parallel test load, fetching them on-the-fly from the browser can
    time out. We pre-download them in Python and serve them via Playwright
    route interception so the browser has zero CDN dependency.
    """
    if any(k.startswith("https://esm.sh/") for k in _CDN_CACHE):
        return
    to_fetch = ["https://esm.sh/mediasoup-client@3"]
    while to_fetch:
        url = to_fetch.pop(0)
        if url in _CDN_CACHE:
            continue
        r = requests.get(url, allow_redirects=True, timeout=30)
        _CDN_CACHE[r.url] = r.text
        if r.url != url:
            _CDN_CACHE[url] = r.text
        for line in r.text.splitlines():
            if 'from "/' in line or 'import "/' in line:
                for part in line.split('"'):
                    if part.startswith("/"):
                        dep = f"https://esm.sh{part}"
                        if dep not in _CDN_CACHE:
                            to_fetch.append(dep)


def _join_mediasoup(page, room_name: str):
    """Load a minimal mediasoup subscriber page and wait for video.

    The mediasoup-demo server validates the Origin header on WebSocket
    connections. We intercept the page load via Playwright routing so
    the browser's origin matches the server, allowing the WebSocket
    handshake to succeed.
    """
    _prefetch_mediasoup_client()

    html = (
        _MEDIASOUP_HTML
        .replace("MEDIASOUP_WS_URL", "wss://172.23.0.10:4443")
        .replace("MEDIASOUP_ROOM_ID", room_name)
    )
    # Intercept the page load so origin matches the WebSocket server.
    page.route("https://172.23.0.10:4443/e2e-viewer", lambda route: route.fulfill(
        status=200, content_type="text/html", body=html,
    ))
    # Serve pre-fetched ESM modules so the browser never hits the CDN.
    def handle_esm(route):
        url = route.request.url
        if url in _CDN_CACHE:
            route.fulfill(status=200, content_type="application/javascript; charset=utf-8", body=_CDN_CACHE[url])
        else:
            route.continue_()
    page.route("https://esm.sh/**", handle_esm)

    page.goto("https://172.23.0.10:4443/e2e-viewer", wait_until="load", timeout=60_000)
    page.wait_for_function("document.title === 'JOINED' || document.title.startsWith('ERROR')", timeout=60_000)
    title = page.title()
    assert not title.startswith("ERROR"), f"Mediasoup join failed: {title}"
    page.wait_for_selector("video", timeout=30_000)


@pytest.mark.xdist_group("mediasoup")
def test_mediasoup_screenshot(mediasoup_infra, test_video_dir, tmp_path):
    _run_screenshot_test(
        sfu_name="mediasoup",
        config="smoke-2s2v.yml",
        network=MEDIASOUP_NETWORK,
        join_fn=lambda page: _join_mediasoup(page, room_name="room-1234"),
        tmp_path=tmp_path,
        test_video_dir=test_video_dir,
        env=PLUGIN_ENV["mediasoup"],
    )
