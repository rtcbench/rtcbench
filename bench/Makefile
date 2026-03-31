IMAGE := bench-runner:latest
REPO_ROOT := $(shell cd .. && pwd)
RUN := docker run --rm --network host -e HOME=$(HOME) \
	-v $(REPO_ROOT):$(REPO_ROOT) -w $(REPO_ROOT)/bench \
	-v $(HOME)/.ssh:$(HOME)/.ssh:ro -v /tmp:/tmp $(IMAGE)

# SSH shorthand for pushing images to remote hosts.
SSH_KEY := $(shell grep ssh_key cluster.yml | awk '{print $$2}')
SSH_USER := $(shell grep ssh_user cluster.yml | awk '{print $$2}')
SSH := ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
	-o LogLevel=ERROR -o ConnectTimeout=10 -i $(SSH_KEY)

# Hosts that need the rtcbench image (sender + receivers).
RTCBENCH_HOSTS := $(shell grep -E '^\s*(sender|-)' cluster.yml | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+')

.PHONY: build build-web build-rtcbench push-rtcbench bench bench-quick bench-run bench-analyze

build:
	docker build -t $(IMAGE) .

build-web:
	docker build -t rtcbench-janus-web:latest docker/janus-web
	docker build -t rtcbench-livekit-web:latest docker/livekit-web
	docker build -t rtcbench-mediasoup-web:latest docker/mediasoup-web

build-rtcbench:
	cd $(REPO_ROOT) && docker build -t rtcbench:latest .

# Only push if the local image ID differs from the remote.
push-rtcbench: build-rtcbench
	@LOCAL_ID=$$(docker images -q rtcbench:latest); \
	for host in $(RTCBENCH_HOSTS); do \
		REMOTE_ID=$$($(SSH) $(SSH_USER)@$$host docker images -q rtcbench:latest 2>/dev/null); \
		if [ "$$LOCAL_ID" = "$$REMOTE_ID" ] && [ -n "$$REMOTE_ID" ]; then \
			echo "rtcbench:latest up-to-date on $$host ($$LOCAL_ID)"; \
		else \
			echo "Pushing rtcbench:latest to $$host..."; \
			docker save rtcbench:latest | $(SSH) $(SSH_USER)@$$host 'docker load && docker image prune -f > /dev/null'; \
		fi; \
	done

bench-quick: build build-web push-rtcbench
	$(RUN) -m bench cluster.yml --sfus janus --clients rtcbench,chromium --quick -v

bench: build build-web push-rtcbench
	$(RUN) -m bench cluster.yml --clients rtcbench,chromium,webrtcperf -v

# Run specific cells or custom args:
#   make bench-run ARGS="--cells 'janus:chromium,webrtcperf;jitsi:webrtcperf' -v"
bench-run: build build-web push-rtcbench
	$(RUN) -m bench cluster.yml $(ARGS)

# Analyze results:
#   make bench-analyze ARGS="results/2026-03-17T10-26-51Z"
bench-analyze: build
	$(RUN) -m bench.analyze $(ARGS)
