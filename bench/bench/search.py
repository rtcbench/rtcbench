"""Binary search for maximum healthy R."""

import logging
import math

from bench.config import log
from bench.experiment import Experiment


def binary_search(ssh, cluster, sfu_name, client_name, min_r, max_r,
                   hard_min_r=1, hard_max_r=None, dry_run=False, run_dir=None):
    """Binary search for the maximum R where all receivers are healthy.

    If the search range was narrowed by a hint and max_r turns out to be
    healthy, the range expands upward toward hard_max_r. If the entire
    hint range is unhealthy, expands downward to hard_min_r.

    Returns (max_healthy_r, search_history).
    """
    if hard_max_r is None:
        hard_max_r = max_r
    lo = min_r
    hi = max_r
    best = 0
    history = []

    log.info("Binary search: SFU=%s Client=%s range=[%d, %d] (hard ceiling=%d)",
             sfu_name, client_name, lo, hi, hard_max_r)

    while lo <= hi:
        if best > 0:
            threshold = max(1, int(math.log2(best)))
            if hi - lo <= threshold:
                if best >= max_r and hard_max_r > max_r:
                    new_hi = min(hard_max_r, best + (best - min_r))
                    log.info("Binary search: hint range exhausted (best=%d >= "
                             "hint_hi=%d), expanding to %d (hard ceiling=%d)",
                             best, max_r, new_hi, hard_max_r)
                    max_r = new_hi
                    hi = new_hi
                    lo = best + 1
                    continue
                log.info("Binary search: converged - range [%d, %d] within "
                         "threshold %d (log2(%d)), stopping with best=%d",
                         lo, hi, threshold, best, best)
                break

        mid = (lo + hi) // 2
        log.info("Binary search: trying R=%d (lo=%d, hi=%d)", mid, lo, hi)

        if dry_run:
            log.info("[dry-run] Would run experiment: SFU=%s Client=%s R=%d",
                     sfu_name, client_name, mid)
            healthy = mid <= 20
        else:
            exp = Experiment(ssh, cluster, sfu_name, client_name, mid, run_dir)
            healthy = exp.run()

        history.append((mid, healthy))
        log.info("Binary search: R=%d -> %s", mid, "HEALTHY" if healthy else "UNHEALTHY")

        if healthy:
            best = mid
            lo = mid + 1
        else:
            hi = mid - 1

    # If hint range was all unhealthy, expand downward to find a healthy R.
    if best == 0 and min_r > hard_min_r:
        log.info("Binary search: hint range [%d, %d] all unhealthy, "
                 "expanding down to [%d, %d]",
                 min_r, max_r, hard_min_r, min_r - 1)
        lo = hard_min_r
        hi = min_r - 1
        while lo <= hi:
            mid = (lo + hi) // 2
            log.info("Binary search: trying R=%d (lo=%d, hi=%d)", mid, lo, hi)

            exp = Experiment(ssh, cluster, sfu_name, client_name, mid, run_dir)
            healthy = exp.run()

            history.append((mid, healthy))
            log.info("Binary search: R=%d -> %s",
                     mid, "HEALTHY" if healthy else "UNHEALTHY")

            if healthy:
                best = mid
                lo = mid + 1
            else:
                hi = mid - 1

    log.info("Binary search result: SFU=%s Client=%s max_R=%d",
             sfu_name, client_name, best)
    return best, history
