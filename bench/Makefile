IMAGE := bench-runner:latest
REPO_ROOT := $(shell cd .. && pwd)
RUN := docker run --rm --network host -e HOME=$(HOME) \
	-v $(REPO_ROOT):$(REPO_ROOT) -w $(REPO_ROOT)/bench \
	-v $(HOME)/.ssh:$(HOME)/.ssh:ro -v /tmp:/tmp $(IMAGE)

.PHONY: build build-web bench bench-quick bench-run bench-analyze

build:
	docker build -t $(IMAGE) .

build-web:
	docker build -t callzip-janus-web:latest docker/janus-web
	docker build -t callzip-livekit-web:latest docker/livekit-web

bench-quick: build build-web
	$(RUN) -m bench cluster.yml --sfus janus --clients callzip,chromium --quick -v

bench: build build-web
	$(RUN) -m bench cluster.yml --clients callzip,chromium,webrtcperf -v

# Run specific cells or custom args:
#   make bench-run ARGS="--cells 'janus:chromium,webrtcperf;jitsi:webrtcperf' -v"
#   make bench-run ARGS="--sfus janus --clients chromium -v"
bench-run: build build-web
	$(RUN) -m bench cluster.yml $(ARGS)

# Analyze results:
#   make bench-analyze ARGS="results/2026-03-17T10-26-51Z"
bench-analyze: build
	$(RUN) -m bench.analyze $(ARGS)
