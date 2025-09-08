.PHONY: all clean install

all:
	go build -v ./cmd/call.zip

install:
	go install -v ./cmd/call.zip

clean:
	rm -f call.zip call.zip.exe
	go clean -cache
	go clean -testcache
	go clean -modcache
