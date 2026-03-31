"""Benchmark CLI - find maximum concurrent viewers (or senders) per SFU."""

import argparse
import datetime
import logging
import math
import os
import sys

import bench.config as cfg
from bench.config import (
    SFUS, CLIENTS, VIEWERS_PER_ROOM, VIDEO_BITRATE_MBPS,
    DEFAULT_MIN_R, DEFAULT_MAX_R, RESULTS_BASE,
    MODE_RECEIVER, MODE_SENDER,
    log,
)
from bench.ssh import SSHRunner
from bench.sfu import stop_sfu, cleanup, setup_machines
from bench.search import binary_search
from bench.results import load_hints, hint_range, print_results, save_results


def parse_cells(cells_str):
    """Parse --cells spec into list of (sfu, client) tuples.

    Format: semicolon-separated groups of 'sfu:client,client,...'
    Example: 'janus:chromium,webrtcperf;jitsi:webrtcperf'
    Returns: [('janus','chromium'), ('janus','webrtcperf'), ('jitsi','webrtcperf')]
    """
    cells = []
    for group in cells_str.split(";"):
        group = group.strip()
        if ":" not in group:
            raise ValueError(f"Bad cell spec '{group}', expected 'sfu:client[,client,...]'")
        sfu, clients_part = group.split(":", 1)
        sfu = sfu.strip()
        if sfu not in SFUS:
            raise ValueError(f"Unknown SFU: {sfu}")
        for c in clients_part.split(","):
            c = c.strip()
            if c not in CLIENTS:
                raise ValueError(f"Unknown client: {c}")
            cells.append((sfu, c))
    return cells


def run_benchmark(cluster_path, cells, min_r, max_r, dry_run,
                   use_hints=True, quick=False, mode=MODE_RECEIVER):
    """Run the benchmark for a list of (sfu, client) cells."""
    cluster = cfg.load_cluster_config(cluster_path, mode=mode)

    if quick:
        cfg.WARMUP_S = 10
        cfg.EXPERIMENT_DURATION_S = 20
        cfg.RENDEZVOUS_LEAD_S = 10
        cfg.TEARDOWN_WAIT_S = 3
        log.info("Quick mode active: warmup=%ds, steady=%ds",
                 cfg.WARMUP_S, cfg.EXPERIMENT_DURATION_S)

    run_id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H-%M-%SZ")
    run_dir = os.path.join(RESULTS_BASE, run_id)
    os.makedirs(run_dir, exist_ok=True)
    log.info("Results directory: %s", run_dir)

    ssh = SSHRunner(cluster["ssh_key"], cluster["ssh_user"], dry_run=dry_run)

    if not dry_run:
        setup_machines(ssh, cluster)

    sfu_host = cluster["sfu"][0]
    initial_hi = cluster.get("initial_hi", {})
    hint_prefix = "sender/" if mode == MODE_SENDER else ""
    hints = load_hints() if use_hints else {}
    if hints:
        log.info("Using hints from previous run to narrow search ranges")

    results = {}
    histories = {}

    cleanup(ssh, cluster)

    label = "S (senders/machine)" if mode == MODE_SENDER else "R (viewers/machine)"
    if mode == MODE_SENDER:
        log.info("Cluster config: senders=%s sfu=%s viewer=%s (mode=sender)",
                 cluster["senders"], sfu_host, cluster["viewer"])
    else:
        log.info("Cluster config: sender=%s sfu=%s receivers=%s (mode=receiver)",
                 cluster["sender"], sfu_host, cluster["receivers"])
    log.info("Cells: %s", [f"{s}/{c}" for s, c in cells])
    log.info("Binary search default range: [%d, %d] (%s)", min_r, max_r, label)
    if initial_hi:
        log.info("Per-client initial_hi overrides: %s", initial_hi)

    for sfu_name, client_name in cells:
            cell_tag = f"{sfu_name}/{client_name}"
            log.info("")
            log.info("=" * 60)
            log.info("Starting cell: %s (mode=%s)", cell_tag, mode)
            log.info("=" * 60)

            cell_max_r = min(initial_hi.get(client_name, max_r), max_r)

            # NIC ceiling
            nic_mbps = cluster.get("nic_mbps")
            if nic_mbps:
                if mode == MODE_SENDER:
                    # Sender mode: SFU receives S*M and sends S*M (to 1 viewer).
                    # Viewer NIC limit: S*M * bitrate. Per-machine: S = nic / (M * bitrate).
                    num_sender_machines = len(cluster["senders"])
                    nic_ceiling = int(nic_mbps / VIDEO_BITRATE_MBPS / num_sender_machines)
                else:
                    num_senders = math.ceil(cell_max_r / VIEWERS_PER_ROOM)
                    nic_ceiling = int(nic_mbps / VIDEO_BITRATE_MBPS) - num_senders
                if nic_ceiling < cell_max_r:
                    log.info("NIC ceiling: %d (from %d Mbps / %.1f Mbps, mode=%s)",
                             nic_ceiling, nic_mbps, VIDEO_BITRATE_MBPS, mode)
                    cell_max_r = min(cell_max_r, nic_ceiling)

            cell_lo, cell_hi = hint_range(
                sfu_name, client_name, min_r, cell_max_r, hints,
                prefix=hint_prefix)

            try:
                max_healthy, history = binary_search(
                    ssh, cluster, sfu_name, client_name,
                    cell_lo, cell_hi,
                    hard_min_r=min_r, hard_max_r=cell_max_r,
                    dry_run=dry_run, run_dir=run_dir,
                    mode=mode,
                )
                results[(sfu_name, client_name)] = max_healthy
                histories[(sfu_name, client_name)] = history
            except Exception as e:
                log.error("Binary search failed for %s: %s", cell_tag, e)
                results[(sfu_name, client_name)] = -1
                histories[(sfu_name, client_name)] = []
            finally:
                stop_sfu(ssh, sfu_host, sfu_name, cluster)

    cleanup(ssh, cluster)
    print_results(results, histories, mode=mode)
    save_results(results, histories, run_dir,
                 update_hints=(min_r != max_r), hint_prefix=hint_prefix)

    return results


def main():
    parser = argparse.ArgumentParser(
        description="Benchmark coordinator for rtcbench")
    parser.add_argument("cluster_config",
                        help="Path to cluster config YAML file")
    parser.add_argument("--mode", choices=[MODE_RECEIVER, MODE_SENDER],
                        default=MODE_RECEIVER,
                        help="Benchmark mode: 'receiver' finds max viewers R_max "
                             "(default), 'sender' finds max senders S_max")
    parser.add_argument("--sfus",
                        default=",".join(SFUS),
                        help=f"Comma-separated SFUs (default: {','.join(SFUS)})")
    parser.add_argument("--clients",
                        default=",".join(CLIENTS),
                        help=f"Comma-separated clients (default: {','.join(CLIENTS)})")
    parser.add_argument("--cells",
                        help="Run specific cells: 'sfu:client,...;sfu:client,...' "
                             "(e.g. 'janus:chromium,webrtcperf;jitsi:webrtcperf'). "
                             "Overrides --sfus/--clients.")
    parser.add_argument("--min-r", type=int, default=DEFAULT_MIN_R)
    parser.add_argument("--max-r", type=int, default=DEFAULT_MAX_R)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--ignore-hints", action="store_true")
    parser.add_argument("--quick", action="store_true",
                        help="Short warmup/steady-state for dev iteration")
    parser.add_argument("-v", "--verbose", action="store_true")

    args = parser.parse_args()

    level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s %(levelname)-5s %(name)s: %(message)s",
        datefmt="%H:%M:%S",
    )

    if args.quick and args.max_r == DEFAULT_MAX_R:
        args.max_r = 15

    if args.cells:
        try:
            cells = parse_cells(args.cells)
        except ValueError as e:
            parser.error(str(e))
    else:
        sfus = [s.strip() for s in args.sfus.split(",")]
        clients = [c.strip() for c in args.clients.split(",")]
        for s in sfus:
            if s not in SFUS:
                parser.error(f"Unknown SFU: {s}")
        for c in clients:
            if c not in CLIENTS:
                parser.error(f"Unknown client: {c}")
        cells = [(s, c) for s in sfus for c in clients]

    results = run_benchmark(
        args.cluster_config,
        cells=cells,
        min_r=args.min_r, max_r=args.max_r,
        dry_run=args.dry_run,
        use_hints=not args.ignore_hints,
        quick=args.quick,
        mode=args.mode,
    )

    if any(v < 0 for v in results.values()):
        sys.exit(1)


if __name__ == "__main__":
    main()
