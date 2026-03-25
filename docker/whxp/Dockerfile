FROM bluenviron/mediamtx:1.12.2

COPY mediamtx.yml /mediamtx.yml

EXPOSE 8889
EXPOSE 8189/udp

HEALTHCHECK --interval=5s --timeout=5s --start-period=10s --retries=12 \
    CMD wget -qO- http://localhost:9997/v3/paths/list || exit 1

ENTRYPOINT ["/mediamtx"]
