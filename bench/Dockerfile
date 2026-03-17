FROM python:3.12-slim-bookworm
RUN apt-get update \
    && apt-get install -y --no-install-recommends openssh-client rsync \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir pyyaml
ENTRYPOINT ["python3"]
