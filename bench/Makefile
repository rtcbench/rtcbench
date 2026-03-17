IMAGE := bench-runner:latest
REPO_ROOT := $(shell cd .. && pwd)
RUN := docker run --rm --network host -e HOME=$(HOME) \
	-v $(REPO_ROOT):$(REPO_ROOT) -w $(REPO_ROOT)/bench \
	-v $(HOME)/.ssh:$(HOME)/.ssh:ro -v /tmp:/tmp $(IMAGE)

.PHONY: build build-web bench bench-quick bench-analyze

build:
	docker build -t $(IMAGE) .

build-web:
	docker build -t callzip-janus-web:latest docker/janus-web
	docker build -t callzip-livekit-web:latest docker/livekit-web

bench-quick: build build-web
	$(RUN) python -m bench cluster.yml --sfus janus --clients callzip,chromium --quick -v

bench: build build-web
	$(RUN) python -m bench cluster.yml --clients callzip,chromium,webrtcperf -v

bench-analyze: build
	$(RUN) python -m bench.analyze $(filter-out $@,$(MAKECMDGOALS))
