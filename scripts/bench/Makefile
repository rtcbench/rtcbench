IMAGE := bench-runner:latest
REPO_ROOT := $(shell cd ../.. && pwd)
RUN := docker run --rm --network host -e HOME=$(HOME) \
	-v $(REPO_ROOT):$(REPO_ROOT) -w $(REPO_ROOT) \
	-v $(HOME)/.ssh:$(HOME)/.ssh:ro \
	-v $(HOME)/.ssh/known_hosts:$(HOME)/.ssh/known_hosts -v /tmp:/tmp $(IMAGE)

# SSH shorthand for pushing images to remote hosts.
SSH_KEY := $(shell grep ssh_key $(REPO_ROOT)/cluster.yml | awk '{print $$2}')
SSH_USER := $(shell grep ssh_user $(REPO_ROOT)/cluster.yml | awk '{print $$2}')
SSH := ssh -o StrictHostKeyChecking=accept-new \
	-o LogLevel=ERROR -o ConnectTimeout=10 -i $(SSH_KEY)

# Hosts that need the rtcbench image (sender + receivers).
RTCBENCH_HOSTS := $(shell grep -E '^\s*(sender|-)' $(REPO_ROOT)/cluster.yml | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+')

.PHONY: build build-web build-rtcbench push-rtcbench known-hosts bench bench-quick bench-run bench-analyze

known-hosts:
	mkdir -p "$(HOME)/.ssh"
	touch "$(HOME)/.ssh/known_hosts"

build:
	docker build -t $(IMAGE) -f $(REPO_ROOT)/docker/bench/Dockerfile $(REPO_ROOT)

build-web:
	docker build -t rtcbench-jitsi-bench-web:latest $(REPO_ROOT)/plugin/jitsi/bench/web
	docker build -t rtcbench-janus-web:latest $(REPO_ROOT)/plugin/janus/bench/web
	docker build -t rtcbench-livekit-web:latest $(REPO_ROOT)/plugin/livekit/bench/web
	docker build -t rtcbench-mediasoup-web:latest $(REPO_ROOT)/plugin/mediasoup/bench/web

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

bench-quick: build build-web push-rtcbench known-hosts
	$(RUN) $(REPO_ROOT)/cluster.yml --hints hints.json --sfus janus --clients rtcbench,chromium --quick --provision --upload-assets --out results/$$(date -u +%Y%m%dT%H%M%SZ)

bench: build build-web push-rtcbench known-hosts
	$(RUN) $(REPO_ROOT)/cluster.yml --hints hints.json --clients rtcbench,chromium,webrtcperf --provision --upload-assets --out results/$$(date -u +%Y%m%dT%H%M%SZ)

# Run specific cells or custom args:
#   make bench-run ARGS="--cells 'janus:chromium,webrtcperf;jitsi:webrtcperf' -v"
bench-run: build build-web push-rtcbench known-hosts
	$(RUN) $(REPO_ROOT)/cluster.yml --hints hints.json --out results/$$(date -u +%Y%m%dT%H%M%SZ) $(ARGS)

# Analyze results:
#   make bench-analyze ARGS="results/2026-03-17T10-26-51Z"
bench-analyze: build known-hosts
	$(RUN) analyze $(ARGS)
