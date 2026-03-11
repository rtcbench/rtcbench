FROM livekit/livekit-server:v1.8 AS livekit

FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl \
 && rm -rf /var/lib/apt/lists/*

COPY --from=livekit /livekit-server /usr/local/bin/livekit-server
COPY livekit.yaml /etc/livekit.yaml

EXPOSE 7880
EXPOSE 7881-7891/udp

HEALTHCHECK --interval=5s --timeout=5s --start-period=10s --retries=12 \
    CMD curl -so /dev/null http://localhost:7880/ 2>/dev/null || exit 1

ENTRYPOINT ["livekit-server"]
CMD ["--config", "/etc/livekit.yaml"]
