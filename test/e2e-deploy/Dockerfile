# Disposable "VPS" for the gofasta deploy end-to-end test.
#
# Plays the role of a fresh Linux server reachable over SSH:
#   - systemd as PID 1 (binary deploys manage the app via systemctl)
#   - openssh-server (gofasta deploy's only transport)
#   - Docker Engine + compose plugin (docker deploys run inside this container)
#   - PostgreSQL (binary deploys need a database on the host itself)
#   - nginx + curl preinstalled so `gofasta deploy setup`'s apt step is a
#     fast no-op instead of a full download on every run
#
# Run with: --privileged --cgroupns=host --tmpfs /run --tmpfs /run/lock
# (systemd and the nested Docker daemon both require it).
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        systemd systemd-sysv dbus \
        openssh-server sudo curl ca-certificates \
        docker.io docker-compose-v2 \
        postgresql \
        nginx \
    && rm -rf /var/lib/apt/lists/*

# The deploy user gofasta connects as: passwordless sudo (setup/install steps
# run `sudo apt-get`, `sudo mkdir`, `sudo systemctl`, ...) and docker group
# membership (the docker method pipes `docker save | ssh ... docker load`
# without sudo).
ARG SSH_PUBKEY
RUN useradd -m -s /bin/bash deploy \
    && usermod -aG docker deploy \
    && echo 'deploy ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/deploy \
    && chmod 440 /etc/sudoers.d/deploy \
    && mkdir -p /home/deploy/.ssh \
    && printf '%s\n' "$SSH_PUBKEY" > /home/deploy/.ssh/authorized_keys \
    && chown -R deploy:deploy /home/deploy/.ssh \
    && chmod 700 /home/deploy/.ssh \
    && chmod 600 /home/deploy/.ssh/authorized_keys

RUN systemctl enable ssh docker postgresql

EXPOSE 22 8080

ENTRYPOINT ["/lib/systemd/systemd"]
