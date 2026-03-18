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

# Hosts that need the callzip image (sender + receivers).
CALLZIP_HOSTS := $(shell grep -E '^\s*(sender|-)' cluster.yml | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+')

.PHONY: build build-web build-callzip push-callzip bench bench-quick bench-run bench-analyze

build:
	docker build -t $(IMAGE) .

build-web:
	docker build -t callzip-janus-web:latest docker/janus-web
	docker build -t callzip-livekit-web:latest docker/livekit-web

build-callzip:
	cd $(REPO_ROOT) && docker build -t callzip:latest .

push-callzip: build-callzip
	@for host in $(CALLZIP_HOSTS); do \
		echo "Pushing callzip:latest to $$host..."; \
		docker save callzip:latest | $(SSH) $(SSH_USER)@$$host docker load; \
	done

bench-quick: build build-web push-callzip
	$(RUN) -m bench cluster.yml --sfus janus --clients callzip,chromium --quick -v

bench: build build-web push-callzip
	$(RUN) -m bench cluster.yml --clients callzip,chromium,webrtcperf -v

# Run specific cells or custom args:
#   make bench-run ARGS="--cells 'janus:chromium,webrtcperf;jitsi:webrtcperf' -v"
bench-run: build build-web push-callzip
	$(RUN) -m bench cluster.yml $(ARGS)

# Analyze results:
#   make bench-analyze ARGS="results/2026-03-17T10-26-51Z"
bench-analyze: build
	$(RUN) -m bench.analyze $(ARGS)
