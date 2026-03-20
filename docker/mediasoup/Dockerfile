FROM node:20-bookworm

# mediasoup compiles a C++ worker during npm install.
RUN apt-get update && \
    apt-get install -y --no-install-recommends python3 python3-pip build-essential curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY package.json ./
RUN npm install --omit=dev

COPY server.js ./

EXPOSE 4443 44444

HEALTHCHECK --interval=5s --timeout=5s --start-period=30s --retries=12 \
    CMD curl -sf http://localhost:4443/health || exit 1

CMD ["node", "server.js"]
