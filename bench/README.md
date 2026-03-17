# bench

Binary search for the maximum concurrent viewers per machine before video quality degrades, across Janus, Jitsi, and LiveKit.

## Quick start

```sh
cd bench
make bench-quick    # callzip + chromium vs Janus, short timings (~5 min)
make bench          # all clients x all SFUs (full run)
```

Both targets build the `bench-runner` Docker image and run inside it.

## Cluster config

Copy `cluster.example.yml` to `cluster.yml` and fill in your machine IPs:

```sh
cp cluster.example.yml cluster.yml
```

## Results

Each run creates a timestamped directory under `results/`:

```
results/2026-03-16T22:00:00/
  experiments.jsonl              # one JSON line per binary search iteration
  sfu=janus/client=callzip/R=213/
    192.168.10.88/               # raw per-viewer stats (rsync'd from receiver)
```

Analyze results:

```sh
make bench-analyze results/2026-03-16T22:00:00/
```