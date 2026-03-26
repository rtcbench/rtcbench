"""SFU container lifecycle management."""

import logging

from bench.config import (
    SFUS, SFU_CONTAINER_NAME, WEB_CONTAINER_NAME, JITSI_IMAGE_TAG,
    CALLZIP_SENDER_CONTAINER, CALLZIP_SENDER_LOAD_CONTAINER,
    CALLZIP_VIEWER_CONTAINER, WRP_CONTAINER_NAME,
    data_ip,
)

log = logging.getLogger("bench")


def start_sfu(ssh, sfu_host, sfu_name, cluster=None):
    """Start the SFU container(s) with host networking."""
    if sfu_name == "jitsi":
        _start_jitsi(ssh, cluster)
        return

    log.info("Starting SFU %s on %s", sfu_name, sfu_host)
    ssh.run(sfu_host,
            f"docker rm -f {SFU_CONTAINER_NAME} {WEB_CONTAINER_NAME} 2>/dev/null || true",
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
        ssh.run(sfu_host,
                f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
                "--ulimit nofile=65536:65536 "
                "callzip-livekit:latest", timeout=60)
        ssh.run(sfu_host,
                "for i in $(seq 20); do curl -sf http://localhost:7880/ >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1",
                timeout=40)
        ssh.run(sfu_host,
                f"docker run -d --name {WEB_CONTAINER_NAME} --network host "
                "callzip-livekit-web:latest", timeout=30)
    elif sfu_name == "mediasoup":
        ws_port = cluster.get("mediasoup_ws_port", 4443) if cluster else 4443
        announced = data_ip(cluster, sfu_host) if cluster else sfu_host
        ssh.run(sfu_host,
                f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
                "--ulimit nofile=65536:65536 "
                f"-e DOMAIN={announced} "
                f"-e MEDIASOUP_ANNOUNCED_ADDRESS={announced} "
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

    for host in [prosody_ip, jicofo_ip, jvb_ip]:
        ssh.run(host, f"docker rm -f {SFU_CONTAINER_NAME} 2>/dev/null || true",
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
            f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
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
            f"docker run -d --name {WEB_CONTAINER_NAME} --network host "
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
            f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
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
        f"-e TZ=UTC"
    )
    ssh.run(jvb_ip,
            f"docker run -d --name {SFU_CONTAINER_NAME} --network host "
            f"--ulimit nofile=65536:65536 "
            f"{jvb_env} jitsi/jvb:{JITSI_IMAGE_TAG}", timeout=60)
    log.info("JVB started on %s", jvb_ip)

    ssh.run(jvb_ip,
            "for i in $(seq 60); do curl -sf http://localhost:8080/about/health >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1",
            timeout=90)
    log.info("JVB healthy on %s - Jitsi fully up", jvb_ip)


def stop_sfu(ssh, sfu_host, sfu_name, cluster=None):
    """Stop the SFU and web frontend containers."""
    if sfu_name == "jitsi":
        _stop_jitsi(ssh, cluster)
        return

    log.info("Stopping SFU %s on %s", sfu_name, sfu_host)
    ssh.run(sfu_host,
            f"docker rm -f {SFU_CONTAINER_NAME} {WEB_CONTAINER_NAME} 2>/dev/null || true",
            check=False, timeout=30)


def _stop_jitsi(ssh, cluster):
    """Stop all Jitsi containers across 3 machines."""
    jitsi_cfg = cluster.get("jitsi", {})
    hosts = [jitsi_cfg["jvb"], jitsi_cfg["prosody_web"], jitsi_cfg["jicofo"]]
    log.info("Stopping Jitsi on %s", hosts)
    for host in hosts:
        ssh.run(host,
                f"docker rm -f {SFU_CONTAINER_NAME} {WEB_CONTAINER_NAME} 2>/dev/null || true",
                check=False, timeout=30)


def stop_all_sfus(ssh, sfu_host, cluster=None):
    """Stop all SFU containers."""
    for sfu_name in SFUS:
        stop_sfu(ssh, sfu_host, sfu_name, cluster)


def cleanup(ssh, cluster):
    """Kill all bench containers on all hosts. Called at start and end of run."""
    containers = (f"{SFU_CONTAINER_NAME} {WEB_CONTAINER_NAME} "
                  f"{CALLZIP_SENDER_CONTAINER} {CALLZIP_SENDER_LOAD_CONTAINER} "
                  f"{CALLZIP_VIEWER_CONTAINER} {WRP_CONTAINER_NAME}")
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

    log.info("Cleaning up bench containers on %d hosts...", len(hosts))
    for host in hosts:
        try:
            ssh.run(host,
                    f"docker rm -f {containers} $(docker ps -aq --filter name={WRP_CONTAINER_NAME}-) 2>/dev/null || true",
                    check=False, timeout=30)
        except Exception as e:
            log.warning("Cleanup failed on %s: %s", host, e)
