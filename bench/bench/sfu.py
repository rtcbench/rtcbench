"""SFU container lifecycle management."""

import logging
import os

from bench.config import (
    SFUS, SFU_CONTAINER_NAME, WEB_CONTAINER_NAME, JITSI_IMAGE_TAG,
    CALLZIP_SENDER_CONTAINER, CALLZIP_SENDER_LOAD_CONTAINER,
    CALLZIP_VIEWER_CONTAINER, WRP_CONTAINER_NAME,
    REMOTE_IVF_DIR,
    data_ip,
)

log = logging.getLogger("bench")

# Paths to pre-built binaries and IVF files (relative to repo root / bench dir)
_BENCH_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BINARIES_DIR = os.path.join(_BENCH_DIR, "binaries")
IVF_DIR = os.path.join(_BENCH_DIR, "ivf")
REMOTE_CALLZIP_BIN = "/tmp/bench-bin/call.zip"
REMOTE_LIVEKIT_BIN = "/tmp/bench-bin/livekit-server"


def setup_machines(ssh, cluster):
    """Push binaries and IVF files to all remote machines.

    Copies:
      - call.zip binary   -> sender/viewer machines at REMOTE_CALLZIP_BIN
      - livekit-server    -> SFU machines at REMOTE_LIVEKIT_BIN
      - IVF files         -> sender machines at REMOTE_IVF_DIR
    """
    sender_hosts = list(cluster.get("senders", []))
    viewer_hosts = [cluster["viewer"]] if "viewer" in cluster else []
    sfu_hosts = list(cluster.get("sfu", []))

    callzip_bin = os.path.join(BINARIES_DIR, "call.zip")
    livekit_bin = os.path.join(BINARIES_DIR, "livekit-server")

    if not os.path.exists(callzip_bin):
        raise FileNotFoundError(
            f"call.zip binary not found at {callzip_bin}. "
            "Build with: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "
            "go build -o bench/binaries/call.zip ./cmd/call.zip")
    if not os.path.exists(livekit_bin):
        raise FileNotFoundError(
            f"livekit-server binary not found at {livekit_bin}. "
            "Copy from docker/livekit/livekit-server")

    # Push call.zip to sender and viewer machines
    for host in set(sender_hosts + viewer_hosts):
        log.info("Pushing call.zip binary to %s", host)
        ssh.run(host, f"mkdir -p {os.path.dirname(REMOTE_CALLZIP_BIN)}", timeout=10)
        ssh.rsync_to(host, callzip_bin, REMOTE_CALLZIP_BIN)
        ssh.run(host, f"chmod +x {REMOTE_CALLZIP_BIN}", timeout=10)

    # Push livekit-server to SFU machines
    for host in sfu_hosts:
        log.info("Pushing livekit-server binary to %s", host)
        ssh.run(host, f"mkdir -p {os.path.dirname(REMOTE_LIVEKIT_BIN)}", timeout=10)
        ssh.rsync_to(host, livekit_bin, REMOTE_LIVEKIT_BIN)
        ssh.run(host, f"chmod +x {REMOTE_LIVEKIT_BIN}", timeout=10)

    # Push IVF files to sender machines.
    # The /opt directory may need root to create, so we try sudo first.
    if os.path.isdir(IVF_DIR) and os.listdir(IVF_DIR):
        for host in sender_hosts:
            log.info("Pushing IVF files to %s:%s", host, REMOTE_IVF_DIR)
            ssh.run(host,
                    f"mkdir -p {REMOTE_IVF_DIR} 2>/dev/null || "
                    f"sudo mkdir -p {REMOTE_IVF_DIR} && sudo chmod 777 {REMOTE_IVF_DIR}",
                    check=False, timeout=10)
            ssh.rsync_to(host, IVF_DIR, REMOTE_IVF_DIR, timeout=300)
    else:
        log.warning("No IVF files found at %s — senders will fail if they need video", IVF_DIR)

    log.info("Machine setup complete: %d senders, %d viewers, %d SFUs",
             len(sender_hosts), len(viewer_hosts), len(sfu_hosts))


def start_sfu(ssh, sfu_host, sfu_name, cluster=None):
    """Start the SFU container(s) with host networking."""
    if sfu_name == "jitsi":
        _start_jitsi(ssh, cluster)
        return

    log.info("Starting SFU %s on %s", sfu_name, sfu_host)
    # Stop any leftover processes first
    ssh.run(sfu_host,
            "pkill -f livekit-server 2>/dev/null || true; "
            "pkill -f janus 2>/dev/null || true; "
            "pkill -f mediasoup 2>/dev/null || true; "
            "sleep 1",
            check=False, timeout=15)

    if sfu_name == "janus":
        ssh.run(sfu_host,
                f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
                "--ulimit nofile=65536:65536 "
                "callzip-janus:latest", timeout=60)
        ssh.run(sfu_host,
                "for i in $(seq 30); do curl -sf http://localhost:8088/janus/info >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1",
                timeout=60)
        ssh.run(sfu_host,
                f"docker run -d --name {WEB_CONTAINER_NAME} --network host "
                "callzip-janus-web:latest", timeout=30)
    elif sfu_name == "livekit":
        # Maximize UDP socket buffers so all ICE sockets get 128 MB receive
        # buffers — prevents kernel drops at S=200+ (which caused GCC to
        # under-estimate bandwidth in earlier experiments).
        if cluster:
            for h in {sfu_host} | set(cluster.get("senders", [])) | {cluster.get("viewer", "")}:
                if h:
                    ssh.run(h,
                            "sudo sysctl -w net.core.rmem_max=134217728 "
                            "net.core.rmem_default=134217728 "
                            "net.core.wmem_max=134217728 "
                            "net.core.wmem_default=134217728",
                            check=False, timeout=10)
        announced = data_ip(cluster, sfu_host) if cluster else sfu_host
        lk_config = (
            "port: 7880\n"
            "rtc:\n"
            "  port_range_start: 10000\n"
            "  port_range_end: 60000\n"
            "  use_external_ip: false\n"
            f"  node_ip: {announced}\n"
            "  congestion_control:\n"
            "    stream_allocator:\n"
            "      min_channel_capacity: 10000000000\n"
            "keys:\n"
            "  devkey: secret\n"
            "room:\n"
            "  auto_create: true\n"
            "  departure_timeout: 5\n"
            "  empty_timeout: 10\n"
            "logging:\n"
            "  level: info\n"
        )
        ssh.write_remote_file(sfu_host, "/tmp/bench-livekit.yaml", lk_config)
        ssh.run(sfu_host,
                f"bash -c 'ulimit -n 65536; nohup {REMOTE_LIVEKIT_BIN} "
                f"--config /tmp/bench-livekit.yaml "
                f"> /tmp/livekit.log 2>&1 & echo $! > /tmp/livekit.pid'",
                timeout=15)
        ssh.run(sfu_host,
                "for i in $(seq 20); do wget -qO- http://localhost:7880/ >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1",
                timeout=40)
        # Start LiveKit web frontend (used by webrtcperf/chromium sender bench).
        ssh.run(sfu_host,
                f"docker rm -f {WEB_CONTAINER_NAME} 2>/dev/null || true; "
                f"docker run -d --name {WEB_CONTAINER_NAME} --network host "
                f"callzip-livekit-web:latest",
                check=False, timeout=30)
        # Force all ICE/media onto the data plane. Without this, Pion
        # gathers candidates from all interfaces and may route media
        # through the 1G management interface instead of 10G data.
        if cluster:
            force_data_plane(ssh, cluster)
    elif sfu_name == "mediasoup":
        ws_port = cluster.get("mediasoup_ws_port", 4443) if cluster else 4443
        announced = data_ip(cluster, sfu_host) if cluster else sfu_host
        # Maximize UDP socket buffers (same as LiveKit) to prevent kernel
        # drops at high sender counts.
        if cluster:
            for h in {sfu_host} | set(cluster.get("senders", [])) | {cluster.get("viewer", "")}:
                if h:
                    ssh.run(h,
                            "sudo sysctl -w net.core.rmem_max=134217728 "
                            "net.core.rmem_default=134217728 "
                            "net.core.wmem_max=134217728 "
                            "net.core.wmem_default=134217728",
                            check=False, timeout=10)
        ssh.run(sfu_host,
                f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
                "--ulimit nofile=65536:65536 "
                f"-e DOMAIN={announced} "
                f"-e MEDIASOUP_ANNOUNCED_ADDRESS={announced} "
                "-e INITIAL_OUTGOING_BITRATE=500000000 "
                "callzip-mediasoup:latest", timeout=60)
        ssh.run(sfu_host,
                f"for i in $(seq 60); do curl -kso /dev/null https://localhost:{ws_port}/ 2>&1 && exit 0; sleep 1; done; exit 1",
                timeout=90)
        ssh.run(sfu_host,
                f"docker run -d --name {WEB_CONTAINER_NAME} --network host "
                "callzip-mediasoup-web:latest", timeout=30)
    log.info("SFU %s is up on %s", sfu_name, sfu_host)


def _start_jitsi(ssh, cluster):
    """Start Jitsi's 4 components across 3 machines with host networking."""
    jitsi_cfg = cluster.get("jitsi", {})
    jvb_ip = jitsi_cfg["jvb"]
    prosody_ip = jitsi_cfg["prosody_web"]
    jicofo_ip = jitsi_cfg["jicofo"]
    xmpp_domain = prosody_ip

    log.info("Starting Jitsi: prosody+web=%s, jicofo=%s, jvb=%s",
             prosody_ip, jicofo_ip, jvb_ip)

    # Maximize UDP socket buffers on all hosts (same as LiveKit/mediasoup).
    all_hosts = {prosody_ip, jicofo_ip, jvb_ip}
    if cluster:
        all_hosts |= set(cluster.get("senders", [])) | {cluster.get("viewer", "")}
    for h in all_hosts:
        if h:
            ssh.run(h,
                    "sudo sysctl -w net.core.rmem_max=134217728 "
                    "net.core.rmem_default=134217728 "
                    "net.core.wmem_max=134217728 "
                    "net.core.wmem_default=134217728",
                    check=False, timeout=10)

    # Use unique container names so all 4 components can live on one machine.
    C_PROSODY = "bench-prosody"
    C_WEB = "bench-jitsi-web"
    C_JICOFO = "bench-jicofo"
    C_JVB = "bench-jvb"
    for host in {prosody_ip, jicofo_ip, jvb_ip}:
        ssh.run(host,
                f"docker rm -f {C_PROSODY} {C_WEB} {C_JICOFO} {C_JVB} 2>/dev/null || true",
                check=False, timeout=15)

    # 1. Prosody
    prosody_env = (
        f"-e AUTH_TYPE=internal "
        f"-e ENABLE_AUTH=0 "
        f"-e ENABLE_GUESTS=1 "
        f"-e XMPP_DOMAIN={xmpp_domain} "
        f"-e XMPP_AUTH_DOMAIN=auth.{xmpp_domain} "
        f"-e XMPP_MUC_DOMAIN=conference.{xmpp_domain} "
        f"-e XMPP_INTERNAL_MUC_DOMAIN=internal-muc.{xmpp_domain} "
        f"-e XMPP_RECORDER_DOMAIN=recorder.{xmpp_domain} "
        f"-e JICOFO_AUTH_PASSWORD=jicofosecret "
        f"-e JVB_AUTH_PASSWORD=jvbsecret "
        f"-e TZ=UTC"
    )
    ssh.run(prosody_ip,
            f"docker run -d --name {C_PROSODY} --network host "
            f"{prosody_env} jitsi/prosody:{JITSI_IMAGE_TAG}", timeout=60)
    log.info("Prosody started on %s", prosody_ip)

    ssh.run(prosody_ip,
            "for i in $(seq 60); do timeout 2 bash -c 'echo > /dev/tcp/localhost/5222' 2>/dev/null && exit 0; sleep 1; done; exit 1",
            timeout=90)
    log.info("Prosody healthy on %s", prosody_ip)

    # 2. Jitsi-Web
    web_env = (
        f"-e ENABLE_AUTH=0 "
        f"-e ENABLE_GUESTS=1 "
        f"-e XMPP_SERVER=127.0.0.1 "
        f"-e XMPP_DOMAIN={xmpp_domain} "
        f"-e XMPP_AUTH_DOMAIN=auth.{xmpp_domain} "
        f"-e XMPP_MUC_DOMAIN=conference.{xmpp_domain} "
        f"-e XMPP_BOSH_URL_BASE=http://127.0.0.1:5280 "
        f"-e PUBLIC_URL=https://{xmpp_domain} "
        f"-e TZ=UTC"
    )
    ssh.run(prosody_ip,
            f"docker run -d --name {C_WEB} --network host "
            f"{web_env} callzip-jitsi-web:latest", timeout=60)
    log.info("Jitsi-Web started on %s", prosody_ip)

    ssh.run(prosody_ip,
            "for i in $(seq 60); do curl -kso /dev/null https://localhost/ 2>/dev/null && exit 0; sleep 1; done; exit 1",
            timeout=90)
    log.info("Jitsi-Web healthy on %s", prosody_ip)

    # 3. Jicofo
    jicofo_env = (
        f"-e AUTH_TYPE=internal "
        f"-e ENABLE_AUTH=0 "
        f"-e XMPP_SERVER={prosody_ip} "
        f"-e XMPP_DOMAIN={xmpp_domain} "
        f"-e XMPP_AUTH_DOMAIN=auth.{xmpp_domain} "
        f"-e XMPP_INTERNAL_MUC_DOMAIN=internal-muc.{xmpp_domain} "
        f"-e XMPP_PORT=5222 "
        f"-e JICOFO_AUTH_USER=focus "
        f"-e JICOFO_AUTH_PASSWORD=jicofosecret "
        f"-e JICOFO_ENABLE_HEALTH_CHECKS=true "
        f"-e TZ=UTC"
    )
    ssh.run(jicofo_ip,
            f"docker run -d --name {C_JICOFO} --network host "
            f"{jicofo_env} jitsi/jicofo:{JITSI_IMAGE_TAG}", timeout=60)
    log.info("Jicofo started on %s", jicofo_ip)

    ssh.run(jicofo_ip,
            "for i in $(seq 60); do curl -sf http://localhost:8888/about/health >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1",
            timeout=90)
    log.info("Jicofo healthy on %s", jicofo_ip)

    # 4. JVB
    jvb_env = (
        f"-e XMPP_SERVER={prosody_ip} "
        f"-e XMPP_DOMAIN={xmpp_domain} "
        f"-e XMPP_AUTH_DOMAIN=auth.{xmpp_domain} "
        f"-e XMPP_INTERNAL_MUC_DOMAIN=internal-muc.{xmpp_domain} "
        f"-e XMPP_PORT=5222 "
        f"-e JVB_AUTH_USER=jvb "
        f"-e JVB_AUTH_PASSWORD=jvbsecret "
        f"-e JVB_ADVERTISE_IPS={data_ip(cluster, jvb_ip)} "
        f"-e JVB_PORT=10000 "
        f"-e JVB_TCP_HARVESTER_DISABLED=true "
        f"-e JVB_STUN_SERVERS= "
        f"-e TZ=UTC"
    )
    ssh.run(jvb_ip,
            f"docker run -d --name {C_JVB} --network host "
            f"--ulimit nofile=65536:65536 "
            f"{jvb_env} jitsi/jvb:{JITSI_IMAGE_TAG}", timeout=60)
    log.info("JVB started on %s", jvb_ip)

    ssh.run(jvb_ip,
            "for i in $(seq 60); do curl -sf http://localhost:8080/about/health >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1",
            timeout=90)
    log.info("JVB healthy on %s - Jitsi fully up", jvb_ip)


def force_data_plane(ssh, cluster):
    """Force all ICE/media traffic onto the data plane via iptables SNAT.

    Problem: Pion gathers ICE candidates from ALL interfaces. On machines
    with separate management (e.g. 130.127.133.x) and data (e.g. 10.10.1.x)
    planes, ICE may select the management-IP candidate pair, routing all
    media through the 1G management interface instead of the 10G data plane.

    Fix: on each machine, SNAT outgoing UDP from the management IP to the
    data IP when destined for the data subnet. This transparently rewrites
    the source so all traffic uses the data plane, regardless of which ICE
    candidate Pion selects. On the SFU, also DROP management-subnet UDP
    on ICE ports as defense in depth.
    """
    data_ips = cluster.get("data_ips", {})
    if not data_ips:
        return

    sample_data = next(iter(data_ips.values()))
    data_subnet = ".".join(sample_data.split(".")[:3]) + ".0/24"
    mgmt_subnet = ".".join(next(iter(data_ips.keys())).split(".")[:3]) + ".0/24"

    # Maximize UDP socket buffers on all machines to prevent kernel drops
    # under high-throughput many-sender loads. Without this, the SFU kernel
    # drops 25-30% of incoming UDP packets from senders at S=200+, causing
    # GCC to under-estimate bandwidth and throttle the viewer connection.
    for mgmt_ip in data_ips.keys():
        ssh.run(mgmt_ip,
                "sudo sysctl -w net.core.rmem_max=134217728 "
                "net.core.rmem_default=134217728 "
                "net.core.wmem_max=134217728 "
                "net.core.wmem_default=134217728",
                check=False, timeout=10)

    # SNAT on every mapped machine: mgmt_ip -> data_ip for UDP to data subnet
    for mgmt_ip, data_ip in data_ips.items():
        ssh.run(mgmt_ip,
                f"sudo iptables -t nat -D POSTROUTING -p udp -s {mgmt_ip} -d {data_subnet} "
                f"-j SNAT --to-source {data_ip} 2>/dev/null; "
                f"sudo iptables -t nat -I POSTROUTING -p udp -s {mgmt_ip} -d {data_subnet} "
                f"-j SNAT --to-source {data_ip}",
                check=False, timeout=10)
    log.info("Data-plane SNAT: %s (applied to %d hosts)", data_subnet, len(data_ips))

    # SFU: also DROP management-subnet ICE as defense in depth
    for sfu_host in cluster["sfu"]:
        for rule in [
            f"sudo iptables -D INPUT -p udp -s {mgmt_subnet} --dport 10000:60000 -j DROP 2>/dev/null; "
            f"sudo iptables -I INPUT -p udp -s {mgmt_subnet} --dport 10000:60000 -j DROP",
            f"sudo iptables -D OUTPUT -p udp -d {mgmt_subnet} --sport 10000:60000 -j DROP 2>/dev/null; "
            f"sudo iptables -I OUTPUT -p udp -d {mgmt_subnet} --sport 10000:60000 -j DROP",
        ]:
            ssh.run(sfu_host, rule, check=False, timeout=10)
    log.info("Data-plane enforcement active on %d hosts + SFU iptables", len(data_ips))


def clear_data_plane(ssh, cluster):
    """Remove all data-plane iptables rules added by force_data_plane."""
    data_ips = cluster.get("data_ips", {})
    if not data_ips:
        return

    sample_data = next(iter(data_ips.values()))
    data_subnet = ".".join(sample_data.split(".")[:3]) + ".0/24"
    mgmt_subnet = ".".join(next(iter(data_ips.keys())).split(".")[:3]) + ".0/24"

    for mgmt_ip, data_ip in data_ips.items():
        ssh.run(mgmt_ip,
                f"sudo iptables -t nat -D POSTROUTING -p udp -s {mgmt_ip} -d {data_subnet} "
                f"-j SNAT --to-source {data_ip} 2>/dev/null || true",
                check=False, timeout=10)

    for sfu_host in cluster["sfu"]:
        for rule in [
            f"sudo iptables -D INPUT -p udp -s {mgmt_subnet} --dport 10000:60000 -j DROP 2>/dev/null || true",
            f"sudo iptables -D OUTPUT -p udp -d {mgmt_subnet} --sport 10000:60000 -j DROP 2>/dev/null || true",
        ]:
            ssh.run(sfu_host, rule, check=False, timeout=10)
    log.info("Data-plane enforcement cleared on %d hosts", len(data_ips))


def stop_sfu(ssh, sfu_host, sfu_name, cluster=None):
    """Stop the SFU and web frontend."""
    if sfu_name == "jitsi":
        _stop_jitsi(ssh, cluster)
        return

    log.info("Stopping SFU %s on %s", sfu_name, sfu_host)
    if sfu_name == "livekit":
        ssh.run(sfu_host,
                "pkill -f livekit-server 2>/dev/null || true; "
                "rm -f /tmp/livekit.pid",
                check=False, timeout=15)
        ssh.run(sfu_host,
                f"docker rm -f {WEB_CONTAINER_NAME} 2>/dev/null || true",
                check=False, timeout=10)
        if cluster:
            clear_data_plane(ssh, cluster)
    else:
        ssh.run(sfu_host,
                f"docker rm -f {SFU_CONTAINER_NAME} {WEB_CONTAINER_NAME} 2>/dev/null || true",
                check=False, timeout=30)


def _stop_jitsi(ssh, cluster):
    """Stop all Jitsi containers."""
    jitsi_cfg = cluster.get("jitsi", {})
    hosts = {jitsi_cfg["jvb"], jitsi_cfg["prosody_web"], jitsi_cfg["jicofo"]}
    log.info("Stopping Jitsi on %s", hosts)
    for host in hosts:
        ssh.run(host,
                "docker rm -f bench-prosody bench-jitsi-web bench-jicofo bench-jvb 2>/dev/null || true",
                check=False, timeout=30)


def stop_all_sfus(ssh, sfu_host, cluster=None):
    """Stop all SFU containers."""
    for sfu_name in SFUS:
        stop_sfu(ssh, sfu_host, sfu_name, cluster)


def cleanup(ssh, cluster):
    """Kill all bench processes on all hosts. Called at start and end of run."""
    hosts = set()
    if "sender" in cluster:
        hosts.add(cluster["sender"])
    if "viewer" in cluster:
        hosts.add(cluster["viewer"])
    hosts.update(cluster["sfu"])
    if "receivers" in cluster:
        hosts.update(cluster["receivers"])
    if "senders" in cluster:
        hosts.update(cluster["senders"])
    jitsi_cfg = cluster.get("jitsi", {})
    for key in ("jvb", "prosody_web", "jicofo"):
        if key in jitsi_cfg:
            hosts.add(jitsi_cfg[key])

    log.info("Cleaning up bench processes on %d hosts...", len(hosts))
    for host in hosts:
        try:
            ssh.run(host,
                    "pkill -f 'call.zip' 2>/dev/null || true; "
                    "pkill -f 'livekit-server' 2>/dev/null || true; "
                    "rm -f /tmp/livekit.pid",
                    check=False, timeout=15)
        except Exception as e:
            log.warning("Cleanup failed on %s: %s", host, e)

    # Clear any stale data-plane iptables rules.
    clear_data_plane(ssh, cluster)
