"""Reads experiments.jsonl and prints R_max summary table."""

import json
import sys
from pathlib import Path


def load_experiments(path):
    """Load experiments.jsonl into a list of dicts."""
    path = Path(path)
    if path.is_dir():
        path = path / "experiments.jsonl"
    if not path.exists():
        print(f"Not found: {path}", file=sys.stderr)
        sys.exit(1)

    experiments = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                experiments.append(json.loads(line))
    return experiments


def compute_r_max(experiments):
    """Find the max healthy R for each (sfu, client) pair."""
    r_max = {}
    for exp in experiments:
        key = f"{exp['sfu']}/{exp['client']}"
        if exp["result"]:
            r_max[key] = max(r_max.get(key, 0), exp["r"])
    return r_max


def print_table(r_max):
    """Print R_max summary table."""
    sfus = ["janus", "jitsi", "livekit"]
    clients = sorted(set(k.split("/")[1] for k in r_max))

    header = f"{'Client':<25s}"
    for sfu in sfus:
        header += f" {sfu:>12s}"
    print(header)
    print("-" * len(header))

    for client in clients:
        row = f"{client:<25s}"
        for sfu in sfus:
            key = f"{sfu}/{client}"
            if key in r_max:
                row += f" {r_max[key]:>12d}"
            else:
                row += f" {'---':>12s}"
        print(row)


def print_failures(experiments):
    """Print all non-OK experiments with reasons."""
    failures = [e for e in experiments if not e["result"]]
    if not failures:
        print("\nNo failures.")
        return

    print(f"\nFailures ({len(failures)}):")
    for exp in failures:
        meta = exp.get("meta", {})
        detail = ""
        if exp["reason"] == "NIC_THROUGHPUT_LOW":
            detail = f" (got {meta.get('nic_mbps', '?')} Mbps, expected {meta.get('expected_mbps', '?')} Mbps)"
        elif exp["reason"] == "QUALITY_DEGRADED":
            detail = f" ({meta.get('healthy_pct', '?')}% healthy)"
        elif exp["reason"] == "OOM":
            detail = ""
        elif "error" in meta:
            detail = f" ({meta['error'][:80]})"
        print(f"  {exp['sfu']}/{exp['client']} R={exp['r']:>5d}: {exp['reason']}{detail}")


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <results-dir-or-jsonl>", file=sys.stderr)
        sys.exit(1)

    experiments = load_experiments(sys.argv[1])
    r_max = compute_r_max(experiments)

    print(f"Experiments: {len(experiments)}")
    print()
    print_table(r_max)
    print_failures(experiments)


if __name__ == "__main__":
    main()
