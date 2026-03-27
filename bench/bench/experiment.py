"""Experiment runner - executes a single (SFU, Client, R) experiment."""

import json
import logging
import math
import os
import time
import traceback

from bench.config import (
    VIEWERS_PER_ROOM, SENDERS_PER_ROOM, WARMUP_S, EXPERIMENT_DURATION_S, TEARDOWN_WAIT_S,
    REMOTE_STATS_DIR, REMOTE_LOG_DIR, REMOTE_IVF_DIR,
    CALLZIP_IMAGE, CALLZIP_SENDER_CONTAINER, CALLZIP_SENDER_LOAD_CONTAINER,
    CALLZIP_VIEWER_CONTAINER,
    WRP_IMAGE, WRP_CONTAINER_NAME, SFU_CONTAINER_NAME, data_ip,
    MIN_BITRATE_BPS, MIN_FPS,
    SENDER_CONFIG, SENDER_LOAD_CONFIG, VIEWER_CONFIGS,
    log,
)
from bench.sfu import start_sfu, stop_sfu
from bench.results import log_experiment


class Experiment:
    """Runs a single (SFU, Client, R) experiment."""

    def __init__(self, ssh, cluster, sfu_name, client_name, r_per_machine, run_dir):
        self.ssh = ssh
        self.cluster = cluster
        self.sfu_name = sfu_name
        self.client_name = client_name
        self.r_per_machine = r_per_machine
        self.run_dir = run_dir
        self.sender_ip = cluster["sender"]
        self.pids = {}  # host -> [pids]

        if sfu_name == "jitsi":
            jitsi_cfg = cluster["jitsi"]
            self.sfu_ip = data_ip(cluster, jitsi_cfg["prosody_web"])
            jitsi_hosts = {jitsi_cfg["jvb"], jitsi_cfg["prosody_web"], jitsi_cfg["jicofo"]}
            self.receiver_ips = [r for r in cluster["receivers"] if r not in jitsi_hosts]
            if not self.receiver_ips:
                raise ValueError("No receiver machines left after reserving Jitsi hosts")
            log.info("Jitsi: serverIP=%s, receivers=%s (excluded %s)",
                     self.sfu_ip, self.receiver_ips, jitsi_hosts)
        else:
            self.sfu_ip = data_ip(cluster, cluster["sfu"][0])
            self.receiver_ips = cluster["receivers"]

        # Multi-room
        max_per_room = VIEWERS_PER_ROOM
        if r_per_machine > max_per_room:
            self.num_rooms = math.ceil(r_per_machine / max_per_room)
            self.viewers_per_room = math.ceil(r_per_machine / self.num_rooms)
            log.info("Multi-room (%s): %d rooms x %d viewers/room = %d total (requested %d)",
                     sfu_name, self.num_rooms, self.viewers_per_room,
                     self.num_rooms * self.viewers_per_room, r_per_machine)
        else:
            self.num_rooms = 1
            self.viewers_per_room = r_per_machine

    def _build_topology(self):
        """Build topology dict for JSONL logging."""
        if self.sfu_name == "jitsi":
            jitsi_cfg = self.cluster["jitsi"]
            sfu_hosts = {
                "prosody": jitsi_cfg["prosody_web"],
                "jitsi-web": jitsi_cfg["prosody_web"],
                "jicofo": jitsi_cfg["jicofo"],
                "jvb": jitsi_cfg["jvb"],
            }
        else:
            sfu_hosts = {SFU_CONTAINER_NAME: self.sfu_ip}

        rooms = []
        for i in range(self.num_rooms):
            room_name = f"room-{1234 + i}" if self.num_rooms > 1 else "room-1234"
            rooms.append({
                "name": room_name,
                "senders": 1,
                "sender_host": self.sender_ip,
                "viewers": self.viewers_per_room,
                "viewer_hosts": list(self.receiver_ips),
            })

        viewer_name = CALLZIP_VIEWER_CONTAINER if self.client_name == "callzip" else WRP_CONTAINER_NAME
        containers = {CALLZIP_SENDER_CONTAINER: self.sender_ip}
        for host in self.receiver_ips:
            containers[f"{viewer_name}@{host}"] = host

        return {
            "total_senders": self.num_rooms,
            "total_viewers": self.r_per_machine * len(self.receiver_ips),
            "num_rooms": self.num_rooms,
            "viewers_per_room": self.viewers_per_room,
            "hosts": {
                "sender": self.sender_ip,
                "sfu": sfu_hosts,
                "receivers": list(self.receiver_ips),
            },
            "containers": containers,
            "rooms": rooms,
        }

    def run(self):
        """Execute the experiment. Returns True if all receivers were healthy."""
        tag = f"{self.sfu_name}/{self.client_name}/R={self.r_per_machine}"
        log.info("=== Experiment %s ===", tag)

        self._health_reason = "OK"
        self._health_meta = {}

        try:
            sfu_host = self.cluster["sfu"][0]
            stop_sfu(self.ssh, sfu_host, self.sfu_name, self.cluster)
            start_sfu(self.ssh, sfu_host, self.sfu_name, self.cluster)

            self._prepare_dirs()
            if self.sfu_name == "janus" and self.num_rooms > 1:
                self._create_janus_rooms()
            self._start_sender()
            self._start_receivers()
            self._wait_experiment()
            healthy = self._check_health()
            self._collect_stats(tag)
            return healthy
        except Exception as e:
            tb = traceback.format_exc()
            log.error("Experiment %s failed: %s", tag, e)
            err_str = str(e)
            if "timed out" in err_str.lower():
                self._health_reason = "OOM"
                self._health_meta = {"error": "SSH timed out (machine likely OOM-thrashed)",
                                     "traceback": tb}
            else:
                self._health_reason = "EXPERIMENT_ERROR"
                self._health_meta = {"error": err_str, "traceback": tb}
            return False
        finally:
            self._teardown()
            log_experiment(
                run_dir=self.run_dir,
                sfu=self.sfu_name,
                client=self.client_name,
                r=self.r_per_machine,
                result=self._health_reason == "OK",
                reason=self._health_reason,
                meta=self._health_meta,
                topology=self._build_topology(),
            )

    def _prepare_dirs(self):
        for host in [self.sender_ip] + self.receiver_ips:
            self.ssh.run(host, f"mkdir -p {REMOTE_STATS_DIR} {REMOTE_LOG_DIR}", timeout=60)
            self.ssh.run(host, f"rm -f {REMOTE_STATS_DIR}/*.jsonl {REMOTE_STATS_DIR}/*.csv 2>/dev/null || true", timeout=60)

    def _create_janus_rooms(self):
        """Create Janus videoroom rooms 1234..1234+N-1 via HTTP API.

        Room IDs match expandRoomName("room-1234", i) which produces
        room-1234, room-1235, room-1236, ... → Janus IDs 1234, 1235, 1236.
        Room 1234 already exists from the config file, so we skip it.
        """
        sfu_host = self.cluster["sfu"][0]
        api = "http://localhost:8088/janus"

        result = self.ssh.run(sfu_host,
            f"curl -s -X POST {api} -H 'Content-Type: application/json' "
            f"-d '{{\"janus\":\"create\",\"transaction\":\"t1\"}}'",
            timeout=15)
        resp = json.loads(result.stdout)
        session_id = resp["data"]["id"]

        result = self.ssh.run(sfu_host,
            f"curl -s -X POST {api}/{session_id} -H 'Content-Type: application/json' "
            f"-d '{{\"janus\":\"attach\",\"plugin\":\"janus.plugin.videoroom\",\"transaction\":\"t2\"}}'",
            timeout=15)
        resp = json.loads(result.stdout)
        handle_id = resp["data"]["id"]

        for i in range(1, self.num_rooms):  # skip 0 — room 1234 exists
            room_id = 1234 + i
            body = json.dumps({
                "request": "create", "room": room_id, "publishers": 4096,
                "bitrate": 4000000, "bitrate_cap": True,
                "videocodec": "vp9", "fir_freq": 10, "permanent": False,
            })
            self.ssh.run(sfu_host,
                f"curl -s -X POST {api}/{session_id}/{handle_id} "
                f"-H 'Content-Type: application/json' "
                f"-d '{{\"janus\":\"message\",\"transaction\":\"r{i}\",\"body\":{body}}}'",
                timeout=15)
        log.info("Created %d Janus videoroom rooms (IDs 1234..%d)", self.num_rooms, 1234 + self.num_rooms - 1)

        self.ssh.run(sfu_host,
            f"curl -s -X POST {api}/{session_id} -H 'Content-Type: application/json' "
            f"-d '{{\"janus\":\"destroy\",\"transaction\":\"t3\"}}'",
            timeout=15)

    def _start_sender(self):
        config_path = "/tmp/bench-sender.yml"
        with open(SENDER_CONFIG) as f:
            self.ssh.write_remote_file(self.sender_ip, config_path, f.read())

        env = (
            f"-e PLUGIN_ID={self.sfu_name} "
            f"-e SERVER_IP={self.sfu_ip} "
            f"-e TOTAL_ROOMS={self.num_rooms} "
            f"-e IVF_DIR={REMOTE_IVF_DIR} "
            f"-e LOG_DIR={REMOTE_LOG_DIR} "
        )
        self.ssh.run(self.sender_ip,
                     f"docker rm -f {CALLZIP_SENDER_CONTAINER} 2>/dev/null || true",
                     check=False, timeout=10)
        self.ssh.run(self.sender_ip,
                     f"docker run -d --name {CALLZIP_SENDER_CONTAINER} --network host "
                     f"--ulimit nofile=65536:65536 "
                     f"{env} "
                     f"-v {REMOTE_IVF_DIR}:{REMOTE_IVF_DIR}:ro "
                     f"-v {config_path}:{config_path}:ro "
                     f"-v {REMOTE_LOG_DIR}:{REMOTE_LOG_DIR} "
                     f"{CALLZIP_IMAGE} {config_path}", timeout=30)
        self.pids.setdefault(self.sender_ip, []).append(CALLZIP_SENDER_CONTAINER)
        log.info("Sender started on %s (container=%s, rooms=%d)",
                 self.sender_ip, CALLZIP_SENDER_CONTAINER, self.num_rooms)

        log.info("Waiting 15s for sender to join and start publishing...")
        time.sleep(15)
        self._start_at = int(time.time())

    def _start_receivers(self):
        if self.client_name == "callzip":
            self._start_callzip_receivers()
        elif self.client_name in ("webrtcperf", "chromium"):
            self._start_webrtcperf_receivers()
        else:
            raise ValueError(f"Unknown client: {self.client_name}")

    def _start_callzip_receivers(self):
        viewer_config = VIEWER_CONFIGS[self.sfu_name]
        with open(viewer_config) as f:
            config_content = f.read()

        env = (
            f"-e SERVER_IP={self.sfu_ip} "
            f"-e VIEWERS_PER_ROOM={self.viewers_per_room} "
            f"-e TOTAL_ROOMS={self.num_rooms} "
            f"-e STATS_JSONL_PATH={REMOTE_STATS_DIR} "
            f"-e LOG_DIR={REMOTE_LOG_DIR} "
        )

        for host in self.receiver_ips:
            config_path = "/tmp/bench-viewer.yml"
            self.ssh.write_remote_file(host, config_path, config_content)

            self.ssh.run(host,
                         f"docker rm -f {CALLZIP_VIEWER_CONTAINER} 2>/dev/null || true",
                         check=False, timeout=10)
            self.ssh.run(host,
                         f"docker run -d --name {CALLZIP_VIEWER_CONTAINER} --network host "
                         f"--ulimit nofile=65536:65536 "
                         f"{env} "
                         f"-v {config_path}:{config_path}:ro "
                         f"-v {REMOTE_STATS_DIR}:{REMOTE_STATS_DIR} "
                         f"-v {REMOTE_LOG_DIR}:{REMOTE_LOG_DIR} "
                         f"{CALLZIP_IMAGE} {config_path}", timeout=30)
            self.pids.setdefault(host, []).append(CALLZIP_VIEWER_CONTAINER)
            log.info("call.zip viewer started on %s (container=%s, R=%d, rooms=%d, per_room=%d)",
                     host, CALLZIP_VIEWER_CONTAINER, self.r_per_machine,
                     self.num_rooms, self.viewers_per_room)

    def _start_webrtcperf_receivers(self):
        max_decoders = 0 if self.client_name == "webrtcperf" else -1

        for host in self.receiver_ips:
            url = self._webrtcperf_url()

            wrp_config = {
                "url": url,
                "sessions": self.r_per_machine,
                "maxVideoDecoders": max_decoders,
                "showPageLog": True,
                "statsInterval": 5,
                "statsPath": f"{REMOTE_STATS_DIR}/wrp-stats.csv",
            }

            if self.num_rooms > 1:
                self._write_wrp_url_handler(host)
                wrp_config["customUrlHandler"] = "/tmp/wrp-url-handler.js"

            config_json = json.dumps(wrp_config)
            self.ssh.write_remote_file(host, "/tmp/wrp-config.json", config_json)
            self.ssh.run(host, f"docker rm -f {WRP_CONTAINER_NAME} 2>/dev/null || true",
                         check=False, timeout=10)

            volumes = (
                f"-v /tmp/wrp-config.json:/config.json:ro "
                f"-v {REMOTE_STATS_DIR}:{REMOTE_STATS_DIR}"
            )
            if self.num_rooms > 1:
                volumes += f" -v /tmp/wrp-url-handler.js:/tmp/wrp-url-handler.js:ro"

            cmd = (
                f"docker run -d --name {WRP_CONTAINER_NAME} --network host "
                f"--shm-size=2g "
                f"{volumes} "
                f"{WRP_IMAGE} "
                f"--run-xvfb /config.json"
            )
            self.ssh.run(host, cmd, timeout=60)
            self.pids.setdefault(host, []).append(WRP_CONTAINER_NAME)
            log.info("%s started on %s (container=%s, sessions=%d, rooms=%d, per_room=%d, decoders=%d)",
                     self.client_name, host, WRP_CONTAINER_NAME,
                     self.r_per_machine, self.num_rooms,
                     self.viewers_per_room, max_decoders)

    def _write_wrp_url_handler(self, host):
        handler_js = self._generate_wrp_url_handler()
        self.ssh.write_remote_file(host, "/tmp/wrp-url-handler.js", handler_js)

    def _generate_wrp_url_handler(self):
        """Generate JS that maps session ID to a room-specific URL.

        Room names use incrementing digits: room-1234, room-1235, room-1236, ...
        For Janus: ?room=1234 → ?room=1235, ?room=1236, ...
        For Jitsi: /room-1234 → /room-1235, /room-1236, ...
        For LiveKit: &room=room-1234 → &room=room-1235, ...
        """
        return f"""\
module.exports = function({{sessions, id, params}}) {{
  const viewersPerRoom = {self.viewers_per_room};
  const roomIndex = Math.floor(id / viewersPerRoom);
  const baseUrl = params.url;
  if (roomIndex === 0) return baseUrl;
  // Increment the trailing number in the room identifier.
  return baseUrl.replace(/(room[=-]?)(\\d+)/, (m, prefix, num) =>
    prefix + (parseInt(num, 10) + roomIndex));
}};
"""

    def _webrtcperf_url(self, room_index=None):
        sfu_ip = self.sfu_ip
        if room_index is not None:
            janus_room_id = 1234 + room_index
            room_name = f"room-{1234 + room_index}"
        else:
            janus_room_id = 1234
            room_name = "room-1234"

        if self.sfu_name == "janus":
            web_port = self.cluster.get("janus_web_port", 8080)
            api_port = self.cluster.get("janus_api_port", 8088)
            return (f"http://{sfu_ip}:{web_port}/"
                    f"?room={janus_room_id}&server=http://{sfu_ip}:{api_port}/janus")
        elif self.sfu_name == "jitsi":
            port = self.cluster.get("jitsi_web_port", 443)
            return (f"https://{sfu_ip}:{port}/{room_name}"
                    f"#config.prejoinConfig.enabled=false"
                    f"&config.p2p.enabled=false"
                    f"&config.startWithAudioMuted=true"
                    f"&config.startWithVideoMuted=true"
                    f"&config.startSilent=true"
                    f"&config.disableDeepLinking=true"
                    f"&config.requireDisplayName=false"
                    f"&config.testing.testMode=true"
                    f"&config.testing.noAutoPlayVideo=true"
                    f"&config.channelLastN=-1"
                    f"&config.notifications=[]"
                    f"&userInfo.displayName=%22bench-viewer%22")
        elif self.sfu_name == "livekit":
            web_port = self.cluster.get("livekit_web_port", 8080)
            ws_port = self.cluster.get("livekit_ws_port", 7880)
            return (f"http://{sfu_ip}:{web_port}/"
                    f"?ws=ws://{sfu_ip}:{ws_port}"
                    f"&room={room_name}&key=devkey&secret=secret")
        elif self.sfu_name == "mediasoup":
            web_port = self.cluster.get("mediasoup_web_port", 8080)
            return (f"https://{sfu_ip}:{web_port}/"
                    f"?roomId={room_name}"
                    f"&produce=false&consume=true"
                    f"&webcam=false&mic=false"
                    f"&forceVP9=true"
                    f"&displayName=bench-viewer")
        else:
            raise ValueError(f"Unknown SFU: {self.sfu_name}")

    def _wait_experiment(self):
        import bench.config as _cfg
        total_s = _cfg.WARMUP_S + _cfg.EXPERIMENT_DURATION_S
        wait_until = self._start_at + total_s
        remaining = wait_until - time.time()
        if remaining > 0:
            log.info("Waiting %.0fs for experiment to complete "
                     "(warmup=%ds + steady=%ds, until %d)...",
                     remaining, _cfg.WARMUP_S, _cfg.EXPERIMENT_DURATION_S, wait_until)
            time.sleep(remaining)

    def _check_health(self):
        """Check health of receivers. Sets _health_reason and _health_meta."""
        self._health_reason = "OK"
        self._health_meta = {}
        if self.client_name in ("webrtcperf", "chromium"):
            return self._check_health_webrtcperf()

        # NIC throughput check
        nic_ok = True
        for host in self.receiver_ips:
            try:
                nic_script = (
                    "DEV=$(ip route get %s | head -1 | awk '{for(i=1;i<=NF;i++) if($i==\"dev\") print $(i+1)}') && "
                    "RX1=$(cat /sys/class/net/$DEV/statistics/rx_bytes) && "
                    "sleep 5 && "
                    "RX2=$(cat /sys/class/net/$DEV/statistics/rx_bytes) && "
                    "echo $DEV $RX1 $RX2"
                ) % self.sfu_ip
                result = self.ssh.run(host, nic_script, timeout=30)
                parts = result.stdout.strip().split()
                dev, rx1, rx2 = parts[0], int(parts[1]), int(parts[2])
                rx_bps = (rx2 - rx1) * 8 / 5.0
                expected_bps = self.r_per_machine * MIN_BITRATE_BPS
                ratio = rx_bps / expected_bps if expected_bps > 0 else 0
                self._health_meta["nic_mbps"] = round(rx_bps / 1e6, 1)
                self._health_meta["expected_mbps"] = round(expected_bps / 1e6, 1)
                self._health_meta["nic_ratio_pct"] = round(ratio * 100, 1)
                log.info("Host %s NIC %s: %.1f Mbps received (expected %.1f Mbps for R=%d, ratio=%.1f%%)",
                         host, dev, rx_bps / 1e6, expected_bps / 1e6,
                         self.r_per_machine, ratio * 100)
                if ratio < 0.8:
                    log.warning("NIC throughput too low: %.1f Mbps < 80%% of expected %.1f Mbps",
                                rx_bps / 1e6, expected_bps / 1e6)
                    self._health_reason = "NIC_THROUGHPUT_LOW"
                    self._health_meta.update({"ratio_pct": round(ratio * 100, 1)})
                    nic_ok = False
            except Exception as e:
                log.error("NIC throughput check failed for %s: %s", host, e)
                self._health_reason = "NIC_CHECK_ERROR"
                self._health_meta = {"error": str(e)}
                nic_ok = False

        if not nic_ok:
            return False

        # JSONL per-viewer quality check
        unhealthy_count = 0
        total_checked = 0

        for host in self.receiver_ips:
            try:
                script = (
                    "python3 -c '"
                    "import json,glob,sys; last={};\n"
                    "[last.update({s[\"viewer\"]:s})"
                    " for f in sorted(glob.glob(\"/dev/shm/bench-stats/*.jsonl\"))"
                    " for line in open(f) if line.strip()"
                    " for s in [json.loads(line)]];\n"
                    "json.dump({v:{\"bitrate_bps\":s[\"bitrate_bps\"],\"fps\":s[\"fps\"]}"
                    " for v,s in last.items()},sys.stdout)'"
                )
                result = self.ssh.run(host, script, timeout=60)
                viewers = json.loads(result.stdout.strip() or "{}")

                for viewer_id, stats in viewers.items():
                    total_checked += 1
                    bitrate = stats["bitrate_bps"]
                    fps = stats["fps"]
                    if bitrate < MIN_BITRATE_BPS or fps < MIN_FPS:
                        unhealthy_count += 1
                        log.warning("Unhealthy viewer on %s: %s - bitrate=%.0f fps=%.1f",
                                    host, viewer_id, bitrate, fps)
                log.info("Host %s: %d viewers checked (from JSONL)", host, len(viewers))
            except Exception as e:
                log.error("JSONL health check failed for %s: %s", host, e)

        expected_total = self.r_per_machine * len(self.receiver_ips)
        self._health_meta.update({
            "viewers_checked": total_checked,
            "viewers_expected": expected_total,
            "viewers_unhealthy": unhealthy_count,
        })

        if total_checked == 0:
            log.warning("No viewers found in JSONL - treating as unhealthy")
            self._health_reason = "NO_VIEWERS"
            return False

        healthy_frac = (total_checked - unhealthy_count) / total_checked
        self._health_meta["healthy_pct"] = round(healthy_frac * 100, 1)
        log.info("Health: %d/%d viewers healthy (%.1f%%), checked %d/%d expected (%.0f%%)",
                 total_checked - unhealthy_count, total_checked, healthy_frac * 100,
                 total_checked, expected_total,
                 total_checked / expected_total * 100 if expected_total > 0 else 0)

        if total_checked < 50:
            log.warning("Only %d viewers reported stats (need >=50)", total_checked)
            self._health_reason = "TOO_FEW_VIEWERS"
            return False

        if unhealthy_count > 0:
            self._health_reason = "QUALITY_DEGRADED"
            return False

        return True

    def _check_health_webrtcperf(self):
        max_decoders = 0 if self.client_name == "webrtcperf" else -1
        all_ok = True
        self._health_reason = "OK"
        self._health_meta = {}

        for host in self.receiver_ips:
            label = f"{host}:{WRP_CONTAINER_NAME}"
            stats_file = f"{REMOTE_STATS_DIR}/wrp-stats.csv"
            try:
                result = self.ssh.run(
                    host,
                    f"docker inspect {WRP_CONTAINER_NAME} --format '{{{{.State.Running}}}}' 2>/dev/null || echo false",
                    timeout=15)
                running = result.stdout.strip() == "true"
                if not running:
                    oom = self.ssh.run(
                        host,
                        f"docker inspect {WRP_CONTAINER_NAME} --format '{{{{.State.OOMKilled}}}}' 2>/dev/null || echo unknown",
                        check=False, timeout=15)
                    is_oom = oom.stdout.strip() == "true"
                    logs = self.ssh.run(
                        host,
                        f"docker logs {WRP_CONTAINER_NAME} 2>&1 | tail -3",
                        check=False, timeout=15)
                    if is_oom:
                        log.warning("%s: OOM killed", label)
                        self._health_reason = "OOM"
                    else:
                        log.warning("%s: container crashed: %s",
                                    label, logs.stdout.strip().replace('\n', ' | '))
                        self._health_reason = "CONTAINER_CRASHED"
                    all_ok = False
                    continue

                result = self.ssh.run(
                    host,
                    f"head -1 {stats_file} 2>/dev/null && echo '---SPLIT---' && tail -1 {stats_file} 2>/dev/null",
                    timeout=15)
                parts = result.stdout.split('---SPLIT---')
                if len(parts) < 2 or not parts[0].strip() or not parts[1].strip():
                    log.warning("%s: no CSV stats data", label)
                    self._health_reason = "NO_STATS"
                    all_ok = False
                    continue

                cols = parts[0].strip().split(',')
                vals = parts[1].strip().split(',')
                if len(cols) != len(vals):
                    log.warning("%s: CSV column/value mismatch (%d vs %d)",
                                label, len(cols), len(vals))
                    self._health_reason = "STATS_PARSE_ERROR"
                    all_ok = False
                    continue

                row = dict(zip(cols, vals))

                bitrate_length = int(float(row.get('videoRecvBitrates_length', '0')))
                if bitrate_length < self.r_per_machine:
                    log.warning("%s: only %d/%d sessions have bitrate data",
                                label, bitrate_length, self.r_per_machine)
                    self._health_reason = "SESSIONS_MISSING"
                    self._health_meta = {"sessions": bitrate_length,
                                         "expected": self.r_per_machine}
                    all_ok = False
                    continue

                bitrate_min = float(row.get('videoRecvBitrates_min', '0'))
                bitrate_mean = float(row.get('videoRecvBitrates_mean', '0'))
                if bitrate_min < MIN_BITRATE_BPS:
                    log.warning("%s: min bitrate %.0f bps < %.0f threshold",
                                label, bitrate_min, MIN_BITRATE_BPS)
                    self._health_reason = "BITRATE_LOW"
                    self._health_meta = {"min_bps": bitrate_min, "threshold_bps": MIN_BITRATE_BPS}
                    all_ok = False
                    continue

                fps_info = ""
                if max_decoders != 0:
                    fps_min = float(row.get('videoRecvFps_min', '0'))
                    fps_info = f", min_fps={fps_min:.1f}"
                    if fps_min < MIN_FPS:
                        log.warning("%s: min FPS %.1f < %.1f threshold",
                                    label, fps_min, MIN_FPS)
                        self._health_reason = "FPS_LOW"
                        self._health_meta = {"min_fps": fps_min, "threshold_fps": MIN_FPS}
                        all_ok = False
                        continue

                log.info("%s: healthy (sessions=%d, min_bps=%.0f, mean_bps=%.0f%s)",
                         label, bitrate_length, bitrate_min, bitrate_mean, fps_info)

            except Exception as e:
                log.error("Health check failed for %s: %s", label, e)
                self._health_reason = "ERROR"
                self._health_meta = {"error": str(e)}
                all_ok = False

        return all_ok

    def _collect_stats(self, tag):
        results_dir = os.path.join(
            self.run_dir,
            f"sfu={self.sfu_name}",
            f"client={self.client_name}",
            f"R={self.r_per_machine}",
        )
        for host in self.receiver_ips:
            local_dir = os.path.join(results_dir, host)
            try:
                self.ssh.rsync_from(host, REMOTE_STATS_DIR, local_dir)
                log.info("Collected stats from %s -> %s", host, local_dir)
            except Exception as e:
                log.error("Failed to collect stats from %s: %s", host, e)

    @staticmethod
    def _is_container_name(pid):
        return not pid.isdigit()

    def _teardown(self):
        log.info("Tearing down experiment...")
        for host, pids in self.pids.items():
            for pid in pids:
                try:
                    if self._is_container_name(pid):
                        self.ssh.run(host, f"docker rm -f {pid} 2>/dev/null || true",
                                     check=False, timeout=30)
                    else:
                        self.ssh.kill(host, pid)
                except Exception as e:
                    log.warning("Teardown failed for %s on %s: %s", pid, host, e)

        time.sleep(TEARDOWN_WAIT_S)
        self.pids.clear()


class SenderExperiment:
    """Runs a single (SFU, S) sender-saturation experiment.

    Topology: S senders (distributed across sender machines) → SFU → 1 viewer.
    Binary-searches for S_max — the maximum concurrent senders before video
    quality degrades on the viewer side.
    """

    def __init__(self, ssh, cluster, sfu_name, client_name, s_per_machine, run_dir):
        self.ssh = ssh
        self.cluster = cluster
        self.sfu_name = sfu_name
        self.client_name = client_name
        self.s_per_machine = s_per_machine
        self.run_dir = run_dir
        self.viewer_ip = cluster["viewer"]
        self.sender_ips = list(cluster["senders"])
        self.pids = {}  # host -> [container_names]

        if sfu_name == "jitsi":
            jitsi_cfg = cluster["jitsi"]
            self.sfu_ip = data_ip(cluster, jitsi_cfg["prosody_web"])
            jitsi_hosts = {jitsi_cfg["jvb"], jitsi_cfg["prosody_web"], jitsi_cfg["jicofo"]}
            self.sender_ips = [s for s in self.sender_ips if s not in jitsi_hosts]
            if not self.sender_ips:
                raise ValueError("No sender machines left after reserving Jitsi hosts")
            log.info("Jitsi sender bench: serverIP=%s, senders=%s (excluded %s)",
                     self.sfu_ip, self.sender_ips, jitsi_hosts)
        else:
            self.sfu_ip = data_ip(cluster, cluster["sfu"][0])

        self.total_senders = s_per_machine * len(self.sender_ips)

        # Multi-room: split senders across rooms to distribute load across
        # SFU workers (each room maps to one worker in mediasoup/livekit).
        if s_per_machine > SENDERS_PER_ROOM:
            self.num_rooms = math.ceil(s_per_machine / SENDERS_PER_ROOM)
            self.senders_per_room = math.ceil(s_per_machine / self.num_rooms)
            log.info("Multi-room sender (%s): %d rooms x %d senders/room = %d per machine (requested %d)",
                     sfu_name, self.num_rooms, self.senders_per_room,
                     self.num_rooms * self.senders_per_room, s_per_machine)
        else:
            self.num_rooms = 1
            self.senders_per_room = s_per_machine

    def _build_topology(self):
        """Build topology dict for JSONL logging."""
        if self.sfu_name == "jitsi":
            jitsi_cfg = self.cluster["jitsi"]
            sfu_hosts = {
                "prosody": jitsi_cfg["prosody_web"],
                "jitsi-web": jitsi_cfg["prosody_web"],
                "jicofo": jitsi_cfg["jicofo"],
                "jvb": jitsi_cfg["jvb"],
            }
        else:
            sfu_hosts = {SFU_CONTAINER_NAME: self.sfu_ip}

        sender_cname = (CALLZIP_SENDER_LOAD_CONTAINER if self.client_name == "callzip"
                        else WRP_CONTAINER_NAME)
        containers = {CALLZIP_VIEWER_CONTAINER: self.viewer_ip}
        for host in self.sender_ips:
            containers[f"{sender_cname}@{host}"] = host

        return {
            "mode": "sender",
            "client": self.client_name,
            "total_senders": self.total_senders,
            "s_per_machine": self.s_per_machine,
            "senders_per_room": self.senders_per_room,
            "total_viewers": self.num_rooms,
            "num_rooms": self.num_rooms,
            "hosts": {
                "viewer": self.viewer_ip,
                "sfu": sfu_hosts,
                "senders": list(self.sender_ips),
            },
            "containers": containers,
            "rooms": [{
                "name": "room-1234",
                "senders": self.total_senders,
                "sender_hosts": list(self.sender_ips),
                "viewers": 1,
                "viewer_host": self.viewer_ip,
            }],
        }

    def run(self):
        """Execute the experiment. Returns True if viewer received all streams healthy."""
        tag = f"{self.sfu_name}/{self.client_name}/S={self.s_per_machine}x{len(self.sender_ips)}"
        log.info("=== Sender Experiment %s (total=%d) ===", tag, self.total_senders)

        self._health_reason = "OK"
        self._health_meta = {}

        try:
            sfu_host = self.cluster["sfu"][0]
            stop_sfu(self.ssh, sfu_host, self.sfu_name, self.cluster)
            start_sfu(self.ssh, sfu_host, self.sfu_name, self.cluster)

            self._prepare_dirs()
            self._start_viewer()
            self._start_senders()
            self._wait_experiment()
            healthy = self._check_health()
            self._collect_stats(tag)
            return healthy
        except Exception as e:
            tb = traceback.format_exc()
            log.error("Sender experiment %s failed: %s", tag, e)
            err_str = str(e)
            if "timed out" in err_str.lower():
                self._health_reason = "OOM"
                self._health_meta = {"error": "SSH timed out (machine likely OOM-thrashed)",
                                     "traceback": tb}
            else:
                self._health_reason = "EXPERIMENT_ERROR"
                self._health_meta = {"error": err_str, "traceback": tb}
            return False
        finally:
            self._teardown()
            log_experiment(
                run_dir=self.run_dir,
                sfu=self.sfu_name,
                client=self.client_name,
                r=self.s_per_machine,
                result=self._health_reason == "OK",
                reason=self._health_reason,
                meta=self._health_meta,
                topology=self._build_topology(),
            )

    def _prepare_dirs(self):
        all_hosts = [self.viewer_ip] + self.sender_ips
        for host in all_hosts:
            self.ssh.run(host, f"mkdir -p {REMOTE_STATS_DIR} {REMOTE_LOG_DIR}", timeout=60)
            self.ssh.run(host, f"rm -f {REMOTE_STATS_DIR}/*.jsonl {REMOTE_STATS_DIR}/*.csv 2>/dev/null || true", timeout=60)

    def _start_viewer(self):
        """Start call.zip viewer(s) on the viewer machine (1 per room)."""
        viewer_config = VIEWER_CONFIGS[self.sfu_name]
        with open(viewer_config) as f:
            config_content = f.read()

        config_path = "/tmp/bench-viewer.yml"
        self.ssh.write_remote_file(self.viewer_ip, config_path, config_content)

        env = (
            f"-e SERVER_IP={self.sfu_ip} "
            f"-e VIEWERS_PER_ROOM=1 "
            f"-e TOTAL_ROOMS={self.num_rooms} "
            f"-e STATS_JSONL_PATH={REMOTE_STATS_DIR} "
            f"-e LOG_DIR={REMOTE_LOG_DIR} "
        )

        self.ssh.run(self.viewer_ip,
                     f"docker rm -f {CALLZIP_VIEWER_CONTAINER} 2>/dev/null || true",
                     check=False, timeout=10)
        self.ssh.run(self.viewer_ip,
                     f"docker run -d --name {CALLZIP_VIEWER_CONTAINER} --network host "
                     f"--ulimit nofile=65536:65536 "
                     f"{env} "
                     f"-v {config_path}:{config_path}:ro "
                     f"-v {REMOTE_STATS_DIR}:{REMOTE_STATS_DIR} "
                     f"-v {REMOTE_LOG_DIR}:{REMOTE_LOG_DIR} "
                     f"{CALLZIP_IMAGE} {config_path}", timeout=30)
        self.pids.setdefault(self.viewer_ip, []).append(CALLZIP_VIEWER_CONTAINER)
        log.info("Viewer started on %s (container=%s, 1 viewer in room-1234)",
                 self.viewer_ip, CALLZIP_VIEWER_CONTAINER)

        log.info("Waiting 10s for viewer to join room...")
        time.sleep(10)

    def _start_senders(self):
        if self.client_name == "callzip":
            self._start_callzip_senders()
        elif self.client_name in ("webrtcperf", "chromium"):
            self._start_webrtcperf_senders()
        else:
            raise ValueError(f"Unknown sender client: {self.client_name}")

    def _start_callzip_senders(self):
        """Start call.zip sender-load containers on each sender machine."""
        with open(SENDER_LOAD_CONFIG) as f:
            config_content = f.read()

        for host in self.sender_ips:
            config_path = "/tmp/bench-sender-load.yml"
            self.ssh.write_remote_file(host, config_path, config_content)

            env = (
                f"-e PLUGIN_ID={self.sfu_name} "
                f"-e SERVER_IP={self.sfu_ip} "
                f"-e SENDERS_PER_ROOM={self.senders_per_room} "
                f"-e TOTAL_ROOMS={self.num_rooms} "
                f"-e IVF_DIR={REMOTE_IVF_DIR} "
                f"-e LOG_DIR={REMOTE_LOG_DIR} "
            )

            self.ssh.run(host,
                         f"docker rm -f {CALLZIP_SENDER_LOAD_CONTAINER} 2>/dev/null || true",
                         check=False, timeout=10)
            self.ssh.run(host,
                         f"docker run -d --name {CALLZIP_SENDER_LOAD_CONTAINER} --network host "
                         f"--ulimit nofile=65536:65536 "
                         f"{env} "
                         f"-v {REMOTE_IVF_DIR}:{REMOTE_IVF_DIR}:ro "
                         f"-v {config_path}:{config_path}:ro "
                         f"-v {REMOTE_LOG_DIR}:{REMOTE_LOG_DIR} "
                         f"{CALLZIP_IMAGE} {config_path}", timeout=30)
            self.pids.setdefault(host, []).append(CALLZIP_SENDER_LOAD_CONTAINER)
            log.info("Sender-load started on %s (container=%s, S=%d, rooms=%d, per_room=%d)",
                     host, CALLZIP_SENDER_LOAD_CONTAINER, self.s_per_machine,
                     self.num_rooms, self.senders_per_room)

        log.info("Waiting 15s for %d senders to join and start publishing...",
                 self.total_senders)
        time.sleep(15)
        self._start_at = int(time.time())

    def _start_webrtcperf_senders(self):
        """Start WebRTCPerf sender containers on each sender machine.

        Each WRP instance runs s_per_machine browser sessions, each publishing
        video into the room via the SFU's web frontend. Uses a Y4M file as
        fake camera input so Chromium encodes at full 1080p30 bitrate instead
        of the low-bitrate default test pattern.
        """
        # chromium decodes incoming video; webrtcperf does not
        max_decoders = 0 if self.client_name == "webrtcperf" else -1
        y4m_path = f"{REMOTE_IVF_DIR}/1080p30.y4m"

        for host in self.sender_ips:
            url = self._webrtcperf_sender_url()

            wrp_config = {
                "url": url,
                "sessions": self.s_per_machine,
                "maxVideoDecoders": max_decoders,
                "showPageLog": True,
                "statsInterval": 5,
                "statsPath": f"{REMOTE_STATS_DIR}/wrp-stats.csv",
            }

            config_json = json.dumps(wrp_config)
            self.ssh.write_remote_file(host, "/tmp/wrp-config.json", config_json)
            self.ssh.run(host, f"docker rm -f {WRP_CONTAINER_NAME} 2>/dev/null || true",
                         check=False, timeout=10)

            cmd = (
                f"docker run -d --name {WRP_CONTAINER_NAME} --network host "
                f"--shm-size=2g "
                f"-v /tmp/wrp-config.json:/config.json:ro "
                f"-v {REMOTE_STATS_DIR}:{REMOTE_STATS_DIR} "
                f"{WRP_IMAGE} "
                f"--run-xvfb /config.json"
            )
            self.ssh.run(host, cmd, timeout=60)
            self.pids.setdefault(host, []).append(WRP_CONTAINER_NAME)
            log.info("%s sender started on %s (container=%s, sessions=%d, decoders=%d)",
                     self.client_name, host, WRP_CONTAINER_NAME,
                     self.s_per_machine, max_decoders)

        log.info("Waiting 15s for %d %s senders to join and start publishing...",
                 self.total_senders, self.client_name)
        time.sleep(15)
        self._start_at = int(time.time())

    def _webrtcperf_sender_url(self):
        """Generate a publish-enabled URL for the SFU's web frontend."""
        sfu_ip = self.sfu_ip

        if self.sfu_name == "janus":
            web_port = self.cluster.get("janus_web_port", 8080)
            api_port = self.cluster.get("janus_api_port", 8088)
            return (f"http://{sfu_ip}:{web_port}/"
                    f"?room=1234&server=http://{sfu_ip}:{api_port}/janus"
                    f"&publish=true")
        elif self.sfu_name == "jitsi":
            port = self.cluster.get("jitsi_web_port", 443)
            return (f"https://{sfu_ip}:{port}/room-1234"
                    f"#config.prejoinConfig.enabled=false"
                    f"&config.p2p.enabled=false"
                    f"&config.startWithAudioMuted=true"
                    f"&config.startWithVideoMuted=false"
                    f"&config.startSilent=true"
                    f"&config.disableDeepLinking=true"
                    f"&config.requireDisplayName=false"
                    f"&config.testing.testMode=true"
                    f"&config.testing.noAutoPlayVideo=true"
                    f"&config.channelLastN=-1"
                    f"&config.notifications=[]"
                    f"&userInfo.displayName=%22bench-sender%22")
        elif self.sfu_name == "livekit":
            web_port = self.cluster.get("livekit_web_port", 8080)
            ws_port = self.cluster.get("livekit_ws_port", 7880)
            return (f"http://{sfu_ip}:{web_port}/"
                    f"?ws=ws://{sfu_ip}:{ws_port}"
                    f"&room=room-1234&key=devkey&secret=secret"
                    f"&publish=true")
        elif self.sfu_name == "mediasoup":
            web_port = self.cluster.get("mediasoup_web_port", 8080)
            return (f"https://{sfu_ip}:{web_port}/"
                    f"?roomId=room-1234"
                    f"&produce=true&consume=true"
                    f"&webcam=true&mic=false"
                    f"&forceVP9=true"
                    f"&displayName=bench-sender")
        else:
            raise ValueError(f"Unknown SFU: {self.sfu_name}")

    def _wait_experiment(self):
        import bench.config as _cfg
        total_s = _cfg.WARMUP_S + _cfg.EXPERIMENT_DURATION_S
        wait_until = self._start_at + total_s
        remaining = wait_until - time.time()
        if remaining > 0:
            log.info("Waiting %.0fs for experiment to complete "
                     "(warmup=%ds + steady=%ds, until %d)...",
                     remaining, _cfg.WARMUP_S, _cfg.EXPERIMENT_DURATION_S, wait_until)
            time.sleep(remaining)

    def _check_health(self):
        """Check health of the single viewer receiving S tracks.

        The viewer spawns one stats entry per incoming track (SSRC-qualified).
        Each track must meet bitrate and FPS thresholds.
        """
        self._health_reason = "OK"
        self._health_meta = {}

        # NIC throughput check on viewer (informational, not a hard fail --
        # the JSONL per-track check below is the authoritative health metric).
        try:
            nic_script = (
                "DEV=$(ip route get %s | head -1 | awk '{for(i=1;i<=NF;i++) if($i==\"dev\") print $(i+1)}') && "
                "RX1=$(cat /sys/class/net/$DEV/statistics/rx_bytes) && "
                "sleep 5 && "
                "RX2=$(cat /sys/class/net/$DEV/statistics/rx_bytes) && "
                "echo $DEV $RX1 $RX2"
            ) % self.sfu_ip
            result = self.ssh.run(self.viewer_ip, nic_script, timeout=30)
            parts = result.stdout.strip().split()
            dev, rx1, rx2 = parts[0], int(parts[1]), int(parts[2])
            rx_bps = (rx2 - rx1) * 8 / 5.0
            expected_bps = self.total_senders * MIN_BITRATE_BPS
            ratio = rx_bps / expected_bps if expected_bps > 0 else 0
            self._health_meta["nic_mbps"] = round(rx_bps / 1e6, 1)
            self._health_meta["expected_mbps"] = round(expected_bps / 1e6, 1)
            self._health_meta["nic_ratio_pct"] = round(ratio * 100, 1)
            log.info("Viewer %s NIC %s: %.1f Mbps received (expected %.1f Mbps for S=%d, ratio=%.1f%%)",
                     self.viewer_ip, dev, rx_bps / 1e6, expected_bps / 1e6,
                     self.total_senders, ratio * 100)
        except Exception as e:
            log.warning("NIC throughput check failed for viewer %s: %s (continuing)", self.viewer_ip, e)

        # JSONL per-track quality check on the single viewer machine
        try:
            script = (
                "python3 -c '"
                "import json,glob,sys; last={};\n"
                "[last.update({s[\"viewer\"]:s})"
                " for f in sorted(glob.glob(\"/dev/shm/bench-stats/*.jsonl\"))"
                " for line in open(f) if line.strip()"
                " for s in [json.loads(line)]];\n"
                "json.dump({v:{\"bitrate_bps\":s[\"bitrate_bps\"],\"fps\":s[\"fps\"]}"
                " for v,s in last.items()},sys.stdout)'"
            )
            result = self.ssh.run(self.viewer_ip, script, timeout=60)
            tracks = json.loads(result.stdout.strip() or "{}")
        except Exception as e:
            log.error("JSONL health check failed for viewer %s: %s", self.viewer_ip, e)
            self._health_reason = "STATS_ERROR"
            self._health_meta = {"error": str(e)}
            return False

        total_checked = len(tracks)
        unhealthy_count = 0
        for track_id, stats in tracks.items():
            bitrate = stats["bitrate_bps"]
            fps = stats["fps"]
            if bitrate < MIN_BITRATE_BPS or fps < MIN_FPS:
                unhealthy_count += 1
                log.warning("Unhealthy track on viewer: %s - bitrate=%.0f fps=%.1f",
                            track_id, bitrate, fps)

        self._health_meta.update({
            "tracks_checked": total_checked,
            "tracks_expected": self.total_senders,
            "tracks_unhealthy": unhealthy_count,
        })

        log.info("Viewer: %d tracks checked (expected %d from %d senders)",
                 total_checked, self.total_senders, self.total_senders)

        if total_checked == 0:
            log.warning("No tracks found in JSONL - treating as unhealthy")
            self._health_reason = "NO_TRACKS"
            return False

        # Allow some tolerance: at least 80% of expected tracks must be present
        if total_checked < self.total_senders * 0.8:
            log.warning("Only %d/%d tracks reporting (need >=80%%)",
                        total_checked, self.total_senders)
            self._health_reason = "TRACKS_MISSING"
            return False

        healthy_frac = (total_checked - unhealthy_count) / total_checked
        self._health_meta["healthy_pct"] = round(healthy_frac * 100, 1)
        log.info("Health: %d/%d tracks healthy (%.1f%%)",
                 total_checked - unhealthy_count, total_checked, healthy_frac * 100)

        if unhealthy_count > 0:
            self._health_reason = "QUALITY_DEGRADED"
            return False

        return True

    def _collect_stats(self, tag):
        results_dir = os.path.join(
            self.run_dir,
            f"sfu={self.sfu_name}",
            f"client={self.client_name}",
            f"S={self.s_per_machine}",
        )
        # Collect viewer stats
        local_dir = os.path.join(results_dir, self.viewer_ip)
        try:
            self.ssh.rsync_from(self.viewer_ip, REMOTE_STATS_DIR, local_dir)
            log.info("Collected viewer stats from %s -> %s", self.viewer_ip, local_dir)
        except Exception as e:
            log.error("Failed to collect viewer stats from %s: %s", self.viewer_ip, e)

    @staticmethod
    def _is_container_name(pid):
        return not pid.isdigit()

    def _teardown(self):
        log.info("Tearing down sender experiment...")
        for host, pids in self.pids.items():
            for pid in pids:
                try:
                    if self._is_container_name(pid):
                        self.ssh.run(host, f"docker rm -f {pid} 2>/dev/null || true",
                                     check=False, timeout=30)
                    else:
                        self.ssh.kill(host, pid)
                except Exception as e:
                    log.warning("Teardown failed for %s on %s: %s", pid, host, e)

        time.sleep(TEARDOWN_WAIT_S)
        self.pids.clear()
