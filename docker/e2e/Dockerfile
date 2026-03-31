# E2E test runner — contains pytest, Playwright, and Docker CLI.
# Talks to the host Docker daemon via socket mount.
#
# Usage (from repo root):
#   docker build -t rtcbench-e2e-runner:latest -f docker/e2e/Dockerfile .
#   docker run --rm \
#     -v /var/run/docker.sock:/var/run/docker.sock \
#     -v $(pwd):$(pwd) -v /tmp:/tmp --network host -w $(pwd) \
#     rtcbench-e2e-runner:latest \
#     -m pytest e2e/ -v -k janus --basetemp=/tmp/pytest-rtcbench

FROM python:3.12-slim-bookworm

# Docker CLI + compose plugin
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" \
       > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin openssh-client rsync make \
    && rm -rf /var/lib/apt/lists/*

# Python test dependencies + Playwright browser
COPY e2e/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt \
    && playwright install --with-deps chromium

ENTRYPOINT ["python"]
