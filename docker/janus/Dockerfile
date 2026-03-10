FROM ubuntu:22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      git \
      pkg-config \
      gengetopt \
      libtool \
      automake \
      libmicrohttpd-dev \
      libjansson-dev \
      libssl-dev \
      libsrtp2-dev \
      libglib2.0-dev \
      libopus-dev \
      libogg-dev \
      libcurl4-openssl-dev \
      libconfig-dev \
      libnice-dev \
 && rm -rf /var/lib/apt/lists/*

# Pin to a specific Janus release for reproducible builds.
# Update JANUS_VERSION to upgrade.
ARG JANUS_VERSION=master
RUN git clone --depth 1 --branch "${JANUS_VERSION}" \
      https://github.com/meetecho/janus-gateway.git /tmp/janus

WORKDIR /tmp/janus
RUN sh autogen.sh \
 && ./configure \
      --prefix=/opt/janus \
      --disable-websockets \
      --disable-data-channels \
      --disable-rabbitmq \
      --disable-mqtt \
      --disable-nanomsg \
      --disable-unix-sockets \
 && make -j"$(nproc)" \
 && make install \
 && make configs

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      curl \
      libmicrohttpd12 \
      libjansson4 \
      libssl3 \
      libsrtp2-1 \
      libglib2.0-0 \
      libopus0 \
      libogg0 \
      libcurl4 \
      libconfig9 \
      libnice10 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /opt/janus /opt/janus

ENV PATH="/opt/janus/bin:${PATH}"

# Override the generated default configs with our custom ones.
COPY janus.jcfg                    /opt/janus/etc/janus/janus.jcfg
COPY janus.transport.http.jcfg     /opt/janus/etc/janus/janus.transport.http.jcfg
COPY janus.plugin.videoroom.jcfg   /opt/janus/etc/janus/janus.plugin.videoroom.jcfg

EXPOSE 8088
# RTP/RTCP media range (must include port 10000 for the static ICE candidate)
EXPOSE 10000-10200/udp

HEALTHCHECK --interval=5s --timeout=5s --start-period=20s --retries=24 \
    CMD curl -sf http://localhost:8088/janus/info > /dev/null || exit 1

CMD ["janus", "--log-stdout"]
