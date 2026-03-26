"""Results logging, hints, and output formatting."""

import json
import logging
import os
import time

from bench.config import HINTS_FILE, MODE_SENDER

log = logging.getLogger("bench")


def log_experiment(run_dir, sfu, client, r, result, reason, meta=None, topology=None):
    """Append one JSONL line per experiment iteration."""
    path = os.path.join(run_dir, "experiments.jsonl")
    entry = {
        "timestamp": time.time(),
        "sfu": sfu,
        "client": client,
        "r": r,
        "result": result,
        "reason": reason,
        "meta": meta or {},
        "topology": topology or {},
    }
    with open(path, "a") as f:
        f.write(json.dumps(entry) + "\n")


def save_results(results, histories, run_dir, update_hints=True, hint_prefix=""):
    """Save results as JSON to the run directory."""
    output = {
        "results": {f"{s}/{c}": v for (s, c), v in results.items()},
        "histories": {f"{s}/{c}": hist for (s, c), hist in histories.items()},
        "timestamp": time.time(),
    }
    if hint_prefix:
        output["mode"] = "sender"
    path = os.path.join(run_dir, "summary.json")
    with open(path, "w") as f:
        json.dump(output, f, indent=2)
    log.info("Results saved to %s", path)

    if update_hints:
        save_hints(results, prefix=hint_prefix)


def print_results(results, histories, mode=None):
    """Print the results table."""
    label = "S_max (senders/machine)" if mode == MODE_SENDER else "R_max (viewers/machine)"

    log.info("")
    log.info("=" * 60)
    log.info("BENCHMARK RESULTS (%s)", label)
    log.info("=" * 60)

    sfus_seen = sorted(set(s for s, _ in results.keys()))
    clients_seen = sorted(set(c for _, c in results.keys()))

    header = f"{'SFU':<12}"
    for client in clients_seen:
        header += f" {client:>12}"
    log.info(header)
    log.info("-" * len(header))

    for sfu in sfus_seen:
        row = f"{sfu:<12}"
        for client in clients_seen:
            val = results.get((sfu, client), -1)
            if val < 0:
                row += f" {'ERROR':>12}"
            else:
                row += f" {val:>12}"
            hist = histories.get((sfu, client), [])
            if hist:
                steps = ", ".join(f"{r}:{'Y' if h else 'N'}" for r, h in hist)
                log.debug("  %s/%s search: %s", sfu, client, steps)
        log.info(row)

    log.info("")

    title = "Sender Benchmark Results" if mode == MODE_SENDER else "Benchmark Results"
    print(f"\n=== {title} ===\n")
    print(header)
    print("-" * len(header))
    for sfu in sfus_seen:
        row = f"{sfu:<12}"
        for client in clients_seen:
            val = results.get((sfu, client), -1)
            if val < 0:
                row += f" {'ERROR':>12}"
            else:
                row += f" {val:>12}"
        print(row)
    print()


def load_hints():
    """Load previous hints from hints.json."""
    try:
        with open(HINTS_FILE) as f:
            data = json.load(f)
        log.info("Loaded hints from %s: %s", HINTS_FILE, data)
        return data
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def save_hints(results, prefix=""):
    """Save discovered max values as hints for future runs.

    prefix="" for receiver mode (keys like "janus/callzip").
    prefix="sender/" for sender mode (keys like "sender/janus/callzip").
    """
    hints = load_hints()
    for (sfu, client), r_max in results.items():
        if r_max > 0:
            hints[f"{prefix}{sfu}/{client}"] = r_max
    hints["_updated"] = time.time()

    os.makedirs(os.path.dirname(HINTS_FILE) or ".", exist_ok=True)
    with open(HINTS_FILE, "w") as f:
        json.dump(hints, f, indent=2)
    log.info("Saved hints to %s", HINTS_FILE)


def hint_range(sfu_name, client_name, default_lo, default_hi, hints,
               margin=0.2, prefix=""):
    """Compute a narrowed binary search range from hints.

    Returns (lo, hi).
    """
    key = f"{prefix}{sfu_name}/{client_name}"
    if key not in hints:
        return default_lo, default_hi

    prev = hints[key]
    lo = max(default_lo, int(prev * (1 - margin)))
    hi = min(default_hi, int(prev * (1 + margin)))
    if lo >= hi:
        lo = max(default_lo, hi - 3)
    log.info("Hint for %s: prev=%d, narrowed range=[%d, %d] (+/-%.0f%%)",
             key, prev, lo, hi, margin * 100)
    return lo, hi
