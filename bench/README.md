# bench

Binary search for the maximum concurrent viewers (or senders) per machine
before video quality degrades, across Janus, Jitsi, LiveKit, and Mediasoup.

## Local test (single machine, no cluster needed)

Runs LiveKit + call.zip senders + a call.zip viewer all on localhost.
Useful for verifying the bench path works before deploying to a cluster.

Prerequisites: Go 1.23+, Docker.

```sh
# From the repo root:

# 1. Build call.zip binary
go build -o bench/binaries/call.zip ./cmd/call.zip

# 2. Build the LiveKit SFU image
docker build -t callzip-livekit:latest docker/livekit

# 3. Generate a test IVF video
mkdir -p bench/ivf
docker build -t callzip:latest .
docker run --rm -v $(pwd)/bench/ivf:/output --entrypoint ffmpeg \
  callzip:latest \
  -f lavfi -i "testsrc2=size=1920x1080:rate=25:duration=10" \
  -c:v libvpx-vp9 -b:v 3500k -g 25 -deadline realtime \
  -f ivf /output/test.ivf -y -loglevel warning

# 4. Run (N = number of senders, default 20)
cd bench
bash local-test.sh 5
```

The script starts LiveKit in Docker, launches N call.zip senders and 1
viewer, waits 40s, then prints how many tracks were received and healthy.

`bench/binaries/` and `bench/ivf/` are gitignored.

## Remote bench (full cluster)

### Quick start

```sh
cd bench
make bench-quick    # callzip + chromium vs Janus, short timings (~5 min)
make bench          # all clients x all SFUs (full run)
```

Both targets build the `bench-runner` Docker image and run inside it.

### Cluster config

Copy `cluster.example.yml` to `cluster.yml` and fill in your machine IPs:

```sh
cp cluster.example.yml cluster.yml
```

### Results

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