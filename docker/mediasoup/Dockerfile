FROM node:22-bookworm AS build

RUN apt-get update && \
    apt-get install -y --no-install-recommends python3 python3-pip build-essential git && \
    rm -rf /var/lib/apt/lists/*

ARG MEDIASOUP_DEMO_COMMIT=0e0d216
RUN git clone https://github.com/versatica/mediasoup-demo.git /src && \
    cd /src && git checkout ${MEDIASOUP_DEMO_COMMIT}

# Build server (TypeScript -> JS).
WORKDIR /src/server
# Disable iceConsentTimeout -- call.zip thin clients do not run an
# interceptor registry, so they never send ICE consent refreshes.
RUN sed -i 's/iceConsentTimeout: 20/iceConsentTimeout: 0/' src/Room.ts
RUN npm ci && npm run typescript:build

# Vite reads server/config.mjs at build time for domain/TLS settings.
# Provide a minimal one so the build succeeds (runtime config is separate).
RUN cp config.example.mjs config.mjs && \
    sed -i "s|ABSOLUTE_PATH_TO_YOUR_TLS_CERTIFICATE_FULLCHAIN|/dev/null|" config.mjs && \
    sed -i "s|ABSOLUTE_PATH_TO_YOUR_TLS_CERTIFICATE_PRIVATE_KEY|/dev/null|" config.mjs

# Build web app (React/Vite -> static files).
WORKDIR /src/app
RUN npm ci --legacy-peer-deps && npm run build

# ------------------------------------------------------------------
FROM node:22-bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends openssl curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Server runtime files.
COPY --from=build /src/server/lib ./lib
COPY --from=build /src/server/node_modules ./node_modules
COPY --from=build /src/server/package.json ./

# Built web app (served by nginx sidecar, but kept here for single-container use).
COPY --from=build /src/app/dist ./public

# Self-signed TLS cert (generated at build time).
RUN openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
    -keyout /app/tls.key -out /app/tls.crt -days 3650 -nodes \
    -subj "/CN=mediasoup-demo"

# Config injected at build time; overridable via bind mount.
COPY config.mjs ./

EXPOSE 4443 44444-44464

HEALTHCHECK --interval=5s --timeout=5s --start-period=60s --retries=12 \
    CMD curl -kso /dev/null https://localhost:4443/ || exit 1

CMD ["node", "lib/index.js"]
