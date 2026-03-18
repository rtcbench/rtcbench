"""Experiment runner - executes a single (SFU, Client, R) experiment."""

import json
import logging
import math
import os
import time
import traceback

from bench.config import (
    VIEWERS_PER_ROOM, WARMUP_S, EXPERIMENT_DURATION_S, TEARDOWN_WAIT_S,
    REMOTE_STATS_DIR, REMOTE_LOG_DIR, REMOTE_IVF_DIR,
    CALLZIP_IMAGE, CALLZIP_SENDER_CONTAINER, CALLZIP_VIEWER_CONTAINER,
    WRP_IMAGE, WRP_CONTAINER_NAME, SFU_CONTAINER_NAME,
    MIN_BITRATE_BPS, MIN_FPS,
    SENDER_CONFIG, VIEWER_CONFIGS,
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
            self.sfu_ip = jitsi_cfg["prosody_web"]
            jitsi_hosts = {jitsi_cfg["jvb"], jitsi_cfg["prosody_web"], jitsi_cfg["jicofo"]}
            self.receiver_ips = [r for r in cluster["receivers"] if r not in jitsi_hosts]
            if not self.receiver_ips:
                raise ValueError("No receiver machines left after reserving Jitsi hosts")
            log.info("Jitsi: serverIP=%s, receivers=%s (excluded %s)",
                     self.sfu_ip, self.receiver_ips, jitsi_hosts)
        else:
            self.sfu_ip = cluster["sfu"][0]
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
            self.ssh.run(host, f"rm -f {REMOTE_STATS_DIR}/*.jsonl {REMOTE_STATS_DIR}/*.csv", timeout=60)

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
        # Shared cache dir prevents each container from converting the same
        # video independently, which fills the 32 GB overlay disk.
        wrp_cache = "/tmp/wrp-cache"

        for host in self.receiver_ips:
            self.ssh.run(host, f"mkdir -p {wrp_cache}", timeout=10)

            if self.num_rooms > 1:
                # One container per room — avoids customUrlHandler which
                # breaks WebRTCPerf's stats aggregation.
                # Start first container alone and wait for it to populate
                # the shared ffmpeg cache, then launch the rest.
                for room_idx in range(self.num_rooms):
                    url = self._webrtcperf_url(room_index=room_idx)
                    container = f"{WRP_CONTAINER_NAME}-{room_idx}"
                    config_path = f"/tmp/wrp-config-{room_idx}.json"
                    stats_file = f"{REMOTE_STATS_DIR}/wrp-stats-{room_idx}.csv"

                    wrp_config = {
                        "url": url,
                        "sessions": self.viewers_per_room,
                        "maxVideoDecoders": max_decoders,
                        "showPageLog": True,
                        "statsInterval": 5,
                        "statsPath": stats_file,
                    }
                    self.ssh.write_remote_file(host, config_path, json.dumps(wrp_config))
                    self.ssh.run(host, f"docker rm -f {container} 2>/dev/null || true",
                                 check=False, timeout=10)
                    cmd = (
                        f"docker run -d --name {container} --network host "
                        f"--shm-size=2g "
                        f"-v {config_path}:/config.json:ro "
                        f"-v {REMOTE_STATS_DIR}:{REMOTE_STATS_DIR} "
                        f"-v {wrp_cache}:/root/.webrtcperf/cache "
                        f"{WRP_IMAGE} "
                        f"--run-xvfb /config.json"
                    )
                    self.ssh.run(host, cmd, timeout=60)
                    self.pids.setdefault(host, []).append(container)
                    if room_idx == 0:
                        # Wait for first container to finish ffmpeg cache
                        # conversion before launching the rest.
                        time.sleep(10)
                log.info("%s started on %s (%d containers, sessions=%d, rooms=%d, per_room=%d, decoders=%d)",
                         self.client_name, host, self.num_rooms,
                         self.r_per_machine, self.num_rooms,
                         self.viewers_per_room, max_decoders)
            else:
                url = self._webrtcperf_url()
                wrp_config = {
                    "url": url,
                    "sessions": self.r_per_machine,
                    "maxVideoDecoders": max_decoders,
                    "showPageLog": True,
                    "statsInterval": 5,
                    "statsPath": f"{REMOTE_STATS_DIR}/wrp-stats.csv",
                }
                self.ssh.write_remote_file(host, "/tmp/wrp-config.json", json.dumps(wrp_config))
                self.ssh.run(host, f"docker rm -f {WRP_CONTAINER_NAME} 2>/dev/null || true",
                             check=False, timeout=10)
                cmd = (
                    f"docker run -d --name {WRP_CONTAINER_NAME} --network host "
                    f"--shm-size=2g "
                    f"-v /tmp/wrp-config.json:/config.json:ro "
                    f"-v {REMOTE_STATS_DIR}:{REMOTE_STATS_DIR} "
                    f"-v {wrp_cache}:/root/.webrtcperf/cache "
                    f"{WRP_IMAGE} "
                    f"--run-xvfb /config.json"
                )
                self.ssh.run(host, cmd, timeout=60)
                self.pids.setdefault(host, []).append(WRP_CONTAINER_NAME)
                log.info("%s started on %s (container=%s, sessions=%d, rooms=%d, per_room=%d, decoders=%d)",
                         self.client_name, host, WRP_CONTAINER_NAME,
                         self.r_per_machine, self.num_rooms,
                         self.viewers_per_room, max_decoders)

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
        else:
            raise ValueError(f"Unknown SFU: {self.sfu_name}")

    def _wait_experiment(self):
        total_s = WARMUP_S + EXPERIMENT_DURATION_S
        wait_until = self._start_at + total_s
        remaining = wait_until - time.time()
        if remaining > 0:
            log.info("Waiting %.0fs for experiment to complete "
                     "(warmup=%ds + steady=%ds, until %d)...",
                     remaining, WARMUP_S, EXPERIMENT_DURATION_S, wait_until)
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

        if self.num_rooms > 1:
            containers = [f"{WRP_CONTAINER_NAME}-{i}" for i in range(self.num_rooms)]
            stats_files = [f"{REMOTE_STATS_DIR}/wrp-stats-{i}.csv" for i in range(self.num_rooms)]
        else:
            containers = [WRP_CONTAINER_NAME]
            stats_files = [f"{REMOTE_STATS_DIR}/wrp-stats.csv"]

        for host in self.receiver_ips:
            total_bitrate_length = 0
            min_bitrate = float('inf')
            mean_bitrate_sum = 0.0
            min_fps = float('inf')

            for container, stats_file in zip(containers, stats_files):
                label = f"{host}:{container}"
                try:
                    result = self.ssh.run(
                        host,
                        f"docker inspect {container} --format '{{{{.State.Running}}}}' 2>/dev/null || echo false",
                        timeout=15)
                    running = result.stdout.strip() == "true"
                    if not running:
                        oom = self.ssh.run(
                            host,
                            f"docker inspect {container} --format '{{{{.State.OOMKilled}}}}' 2>/dev/null || echo unknown",
                            check=False, timeout=15)
                        is_oom = oom.stdout.strip() == "true"
                        logs = self.ssh.run(
                            host,
                            f"docker logs {container} 2>&1 | tail -3",
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
                    bl = int(float(row.get('videoRecvBitrates_length', '0')))
                    total_bitrate_length += bl
                    bmin = float(row.get('videoRecvBitrates_min', '0'))
                    bmean = float(row.get('videoRecvBitrates_mean', '0'))
                    if bmin < min_bitrate:
                        min_bitrate = bmin
                    mean_bitrate_sum += bmean * bl

                    if max_decoders != 0:
                        fmin = float(row.get('videoRecvFps_min', '0'))
                        if fmin < min_fps:
                            min_fps = fmin

                except Exception as e:
                    log.error("Health check failed for %s: %s", label, e)
                    self._health_reason = "ERROR"
                    self._health_meta = {"error": str(e)}
                    all_ok = False

            if not all_ok:
                return False

            if total_bitrate_length < self.r_per_machine:
                log.warning("%s: only %d/%d sessions have bitrate data",
                            host, total_bitrate_length, self.r_per_machine)
                self._health_reason = "SESSIONS_MISSING"
                self._health_meta = {"sessions": total_bitrate_length,
                                     "expected": self.r_per_machine}
                return False

            if min_bitrate < MIN_BITRATE_BPS:
                log.warning("%s: min bitrate %.0f bps < %.0f threshold",
                            host, min_bitrate, MIN_BITRATE_BPS)
                self._health_reason = "BITRATE_LOW"
                self._health_meta = {"min_bps": min_bitrate, "threshold_bps": MIN_BITRATE_BPS}
                return False

            fps_info = ""
            if max_decoders != 0 and min_fps < float('inf'):
                fps_info = f", min_fps={min_fps:.1f}"
                if min_fps < MIN_FPS:
                    log.warning("%s: min FPS %.1f < %.1f threshold",
                                host, min_fps, MIN_FPS)
                    self._health_reason = "FPS_LOW"
                    self._health_meta = {"min_fps": min_fps, "threshold_fps": MIN_FPS}
                    return False

            mean_bitrate = mean_bitrate_sum / total_bitrate_length if total_bitrate_length else 0
            log.info("%s: healthy (sessions=%d, min_bps=%.0f, mean_bps=%.0f%s)",
                     host, total_bitrate_length, min_bitrate, mean_bitrate, fps_info)

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
