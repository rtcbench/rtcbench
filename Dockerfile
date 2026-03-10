FROM golang:1.24-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /call.zip ./cmd/call.zip

FROM debian:bookworm-slim

# ffmpeg: generates the VP9/IVF test video used by the smoke-test script.
# ca-certificates: required for HTTPS connections.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ffmpeg \
      ca-certificates \
      curl \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /call.zip /usr/local/bin/call.zip

RUN mkdir -p /var/log/call-zip /test-videos

ENTRYPOINT ["/usr/local/bin/call.zip"]
