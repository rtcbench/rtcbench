.PHONY: all clean clean-ci clean-screenshots install test \
	e2e e2e-janus e2e-jitsi e2e-livekit e2e-mediasoup screenshots \
	_build-all _build-e2e-runner _build-janus _build-jitsi _build-livekit _build-mediasoup \
	_e2e-run remote-ci remote-run remote-seed remote-status remote-clean remote-estimate \
	ci e2e-coverage

VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

all:
	go build -v $(LDFLAGS) ./cmd/rtcbench

install:
	go install -v $(LDFLAGS) ./cmd/rtcbench

test:
	go vet ./...
	go test -v ./...

# ---------------------------------------------------------------------------
# e2e tests (Python/pytest)
# ---------------------------------------------------------------------------
# Infrastructure (janus/jitsi/livekit) runs in Docker Compose; rtcbench runs
# as a per-test Docker container managed by pytest fixtures. New scenarios are
# discovered automatically — no Makefile changes required.

E2E_RUNNER := rtcbench-e2e-runner:latest
BASETEMP := /tmp/pytest-rtcbench
E2E_RUN := docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v $(CURDIR):$(CURDIR) -v /tmp:/tmp --network host --add-host=host.docker.internal:host-gateway -w $(CURDIR) $(E2E_RUNNER)

_build-e2e-runner:
	docker build -t $(E2E_RUNNER) -f docker/e2e/Dockerfile .

e2e: _build-all _build-e2e-runner
	$(E2E_RUN) -m pytest e2e/ -v -n auto --dist=loadgroup --basetemp=$(BASETEMP)

e2e-janus: _build-janus _build-e2e-runner
	$(E2E_RUN) -m pytest e2e/ -v -k janus --basetemp=$(BASETEMP)

e2e-jitsi: _build-jitsi _build-e2e-runner
	$(E2E_RUN) -m pytest e2e/ -v -k jitsi --basetemp=$(BASETEMP)

e2e-livekit: _build-livekit _build-e2e-runner
	$(E2E_RUN) -m pytest e2e/ -v -k livekit --basetemp=$(BASETEMP)

e2e-mediasoup: _build-mediasoup _build-e2e-runner
	$(E2E_RUN) -m pytest e2e/ -v -k mediasoup --basetemp=$(BASETEMP)

# Run arbitrary pytest args in e2e runner (used by remote_e2e.py)
_e2e-run: _build-e2e-runner
	$(E2E_RUN) -m pytest $(PYTEST_ARGS)

e2e-coverage: _build-e2e-runner
	$(E2E_RUN) e2e/check_coverage.py

SCREENSHOTS_DIR := e2e/screenshots

screenshots: _build-all _build-e2e-runner
	sudo rm -rf $(BASETEMP)
	$(E2E_RUN) -m pytest e2e/test_screenshot.py -v -n auto --dist=loadgroup --basetemp=$(BASETEMP)
	sudo chmod -R a+rX $(BASETEMP)
	$(eval FOLDER := $(shell date +%Y%m%d-%H%M%S))
	mkdir -p $(SCREENSHOTS_DIR)/$(FOLDER)/janus $(SCREENSHOTS_DIR)/$(FOLDER)/jitsi $(SCREENSHOTS_DIR)/$(FOLDER)/livekit $(SCREENSHOTS_DIR)/$(FOLDER)/mediasoup
	cp /tmp/pytest-rtcbench/popen-gw*/test_janus_screenshot*/*.png $(SCREENSHOTS_DIR)/$(FOLDER)/janus/ 2>/dev/null || true
	cp /tmp/pytest-rtcbench/popen-gw*/test_jitsi_screenshot*/*.png $(SCREENSHOTS_DIR)/$(FOLDER)/jitsi/ 2>/dev/null || true
	cp /tmp/pytest-rtcbench/popen-gw*/test_livekit_screenshot*/*.png $(SCREENSHOTS_DIR)/$(FOLDER)/livekit/ 2>/dev/null || true
	cp /tmp/pytest-rtcbench/popen-gw*/test_mediasoup_screenshot*/*.png $(SCREENSHOTS_DIR)/$(FOLDER)/mediasoup/ 2>/dev/null || true
	@echo ""
	@echo "Screenshots saved to $(SCREENSHOTS_DIR)/$(FOLDER)/"
	@echo "View: http://$$(hostname -I | awk '{print $$1}'):8099/verify.html?dir=$(FOLDER)"
	@echo "Run: ./scripts/serve-screenshots.sh"

_build-all:
	docker build -t rtcbench:latest .
	docker build -t rtcbench-janus:latest docker/janus
	docker build -t rtcbench-jitsi-web:latest docker/jitsi
	docker build -t rtcbench-livekit:latest docker/livekit
	docker build -t rtcbench-mediasoup:latest docker/mediasoup

_build-janus:
	docker build -t rtcbench:latest .
	docker build -t rtcbench-janus:latest docker/janus

_build-jitsi:
	docker build -t rtcbench:latest .
	docker build -t rtcbench-jitsi-web:latest docker/jitsi

_build-livekit:
	docker build -t rtcbench:latest .
	docker build -t rtcbench-livekit:latest docker/livekit

_build-mediasoup:
	docker build -t rtcbench:latest .
	docker build -t rtcbench-mediasoup:latest docker/mediasoup

# ---------------------------------------------------------------------------
# Remote distributed e2e (via ssh-parallel-test)
# ---------------------------------------------------------------------------
# Uses spt from the ssh-parallel-test repo to distribute e2e tests across
# remote machines. Runs inside the spt Docker image — control machine
# only needs Docker + make. SSH keys are mounted read-only.

SPT_IMAGE := ghcr.io/ioqr/ssh-parallel-test:latest
REMOTE_CFG ?= scripts/remote-e2e/config.yml
SPT_RUN := docker run --rm -t -e HOME=$(HOME) -e SSH_AUTH_SOCK=/ssh-agent -v $(SSH_AUTH_SOCK):/ssh-agent -v /var/run/docker.sock:/var/run/docker.sock -v $(CURDIR):$(CURDIR) -v /tmp:/tmp -v $(HOME)/.ssh:$(HOME)/.ssh:ro -v $(HOME)/.ssh-parallel-test:$(HOME)/.ssh-parallel-test --network host -w $(CURDIR) $(SPT_IMAGE)
REMOTE_GROUP ?=
ifneq ($(REMOTE_GROUP),)
SPT_GROUP := -g $(REMOTE_GROUP)
endif

remote-seed:
	$(SPT_RUN) -c $(REMOTE_CFG) seed

remote-run:
	$(SPT_RUN) -c $(REMOTE_CFG) run $(SPT_GROUP)

remote-ci:
	$(MAKE) test
	$(SPT_RUN) -c $(REMOTE_CFG) run $(SPT_GROUP)

remote-status:
	$(SPT_RUN) -c $(REMOTE_CFG) status

remote-clean:
	$(SPT_RUN) -c $(REMOTE_CFG) clean

remote-estimate:
	$(SPT_RUN) -c $(REMOTE_CFG) estimate

# ---------------------------------------------------------------------------
# CI
# ---------------------------------------------------------------------------

ci:
	$(MAKE) test e2e

clean-ci:
	docker compose --project-name rtcbench-janus --profile janus down -v --remove-orphans 2>/dev/null || true
	docker compose --project-name rtcbench-jitsi --profile jitsi down -v --remove-orphans 2>/dev/null || true
	docker compose --project-name rtcbench-livekit --profile livekit down -v --remove-orphans 2>/dev/null || true
	docker compose --project-name rtcbench-mediasoup --profile mediasoup down -v --remove-orphans 2>/dev/null || true
	docker rmi -f rtcbench:latest rtcbench-janus:latest rtcbench-jitsi-web:latest rtcbench-livekit:latest rtcbench-mediasoup:latest rtcbench-e2e-runner:latest 2>/dev/null || true
	sudo rm -rf $(BASETEMP)

clean-screenshots:
	rm -rf e2e/screenshots/*/

clean:
	rm -f rtcbench rtcbench.exe
	go clean -cache
	go clean -testcache
	go clean -modcache
