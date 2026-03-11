.PHONY: all clean clean-ci install test \
	e2e e2e-janus e2e-jitsi \
	_build-all _build-janus _build-jitsi \
	ci cztest-client cztest-server cztest-payload-zip

all:
	go build -v ./cmd/call.zip

install:
	go install -v ./cmd/call.zip

cztest-client:
	go build -v ./cmd/cztest-client

cztest-server:
	go build -v ./cmd/cztest-server

cztest-payload:
	@test -f cztest-config.yml || (echo "missing ./cztest-config.yml" && exit 1)
	rm -rf target/cztest-build target/cztest-payload.zip
	mkdir -p target/cztest-build
	GOOS=darwin GOARCH=arm64 go build -v -o target/cztest-build/cztest-binary-os_darwin-arch_arm64 ./cmd/call.zip
	GOOS=linux  GOARCH=amd64 go build -v -o target/cztest-build/cztest-binary-os_linux-arch_amd64  ./cmd/call.zip
	cp -f cztest-config.yml target/cztest-build/config.yml
	cd target/cztest-build && zip -9 -r ../cztest-payload.zip config.yml cztest-binary-os_darwin-arch_arm64 cztest-binary-os_linux-arch_amd64
	@echo "*** DONE ***\nPlease upload ./target/cztest-payload.zip to the cztest server."

test:
	go test -v ./...

# ---------------------------------------------------------------------------
# e2e tests (Python/pytest)
# ---------------------------------------------------------------------------
# Infrastructure (janus/jitsi) runs in Docker Compose; call.zip runs as a
# per-test Docker container managed by pytest fixtures. New scenarios are
# discovered automatically — no Makefile changes required.

e2e: _build-all
	pip install -q -r e2e/requirements.txt
	python -m pytest e2e/ -v -n auto --dist=loadgroup

e2e-janus: _build-janus
	pip install -q -r e2e/requirements.txt
	python -m pytest e2e/ -v -k janus

e2e-jitsi: _build-jitsi
	pip install -q -r e2e/requirements.txt
	python -m pytest e2e/ -v -k jitsi

_build-all:
	docker build -t callzip:latest .
	docker build -t callzip-janus:latest docker/janus
	docker build -t callzip-jitsi-web:latest docker/jitsi

_build-janus:
	docker build -t callzip:latest .
	docker build -t callzip-janus:latest docker/janus

_build-jitsi:
	docker build -t callzip:latest .
	docker build -t callzip-jitsi-web:latest docker/jitsi

# ---------------------------------------------------------------------------
# CI
# ---------------------------------------------------------------------------

ci:
	$(MAKE) test e2e

clean-ci:
	docker compose --project-name callzip --profile janus --profile jitsi \
		down -v --remove-orphans 2>/dev/null || true
	docker rmi -f callzip:latest callzip-janus:latest callzip-jitsi-web:latest 2>/dev/null || true

clean:
	rm -f call.zip call.zip.exe cztest-client cztest-client.exe cztest-server cztest-server.exe
	rm -rf target/cztest-build target/cztest-payload.zip
	go clean -cache
	go clean -testcache
	go clean -modcache
