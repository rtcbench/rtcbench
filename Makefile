.PHONY: all clean install test

all:
	go build -v ./cmd/call.zip

install:
	go install -v ./cmd/call.zip

test:
	go test -v ./...

clean:
	rm -f call.zip call.zip.exe
	go clean -cache
	go clean -testcache
	go clean -modcache
