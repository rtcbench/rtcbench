.PHONY: all clean clean-ci install test smoke-janus smoke-jitsi ci cztest-client cztest-server cztest-payload-zip

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

smoke-janus:
	docker build -t callzip:latest .
	docker build -t callzip-janus:latest docker/janus
	docker compose --profile janus up --exit-code-from janus-smoke-test --abort-on-container-exit
	docker compose --profile janus down -v

smoke-jitsi:
	docker build -t callzip:latest .
	docker build -t callzip-jitsi-web:latest docker/jitsi
	docker compose --profile jitsi up --exit-code-from jitsi-smoke-test --abort-on-container-exit
	docker compose --profile jitsi down -v

ci:
	@if $(MAKE) --no-print-directory test smoke-janus smoke-jitsi; then \
		echo ""; \
		echo "PASS"; \
	else \
		echo ""; \
		echo "FAIL"; \
		exit 1; \
	fi

clean-ci:
	docker compose --profile janus down -v --remove-orphans 2>/dev/null || true
	docker compose --profile jitsi down -v --remove-orphans 2>/dev/null || true
	docker rmi -f callzip:latest callzip-janus:latest callzip-jitsi-web:latest 2>/dev/null || true

clean:
	rm -f call.zip call.zip.exe cztest-client cztest-client.exe cztest-server cztest-server.exe
	rm -rf target/cztest-build target/cztest-payload.zip
	go clean -cache
	go clean -testcache
	go clean -modcache