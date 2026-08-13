#!/usr/bin/env bash
# End-to-end test for `gofasta deploy` against a disposable Docker "VPS".
#
# Scaffolds a real project, then exercises the full deploy surface over SSH
# against a privileged container running systemd + sshd + Docker + PostgreSQL:
#
#   docker method  : setup, first deploy, repeat deploy (container/volume
#                    reuse), status (text + --json), induced-failure deploy
#                    with automatic rollback, explicit rollback
#   binary method  : setup, first deploy on a FRESH server, repeat deploy,
#                    status, explicit rollback   (separate container)
#
# Invoked by `make deploy-e2e` (part of `make preflight`) and by the
# deploy-e2e CI job. Requires Docker and network access.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOFASTA_BIN="${GOFASTA_BIN:-$CLI_DIR/bin/gofasta}"

VPS_IMAGE=gofasta-e2e-vps
DOCKER_VPS=gofasta-e2e-vps-docker
BINARY_VPS=gofasta-e2e-vps-binary
SSH_PORT_DOCKER=42222
APP_PORT_DOCKER=48080
SSH_PORT_BINARY=42223
APP_PORT_BINARY=48081

WORK_DIR="$(mktemp -d /tmp/gofasta-deploy-e2e.XXXXXX)"
PROJECT_DIR="$WORK_DIR/e2eapp"
SSH_KEY="$WORK_DIR/id_e2e"
AGENT_PID=""

# ── logging ──────────────────────────────────────────────────────────────────
log()  { printf '\n\033[1m── e2e: %s\033[0m\n' "$*"; }
pass() { printf '\033[32m   ✓ %s\033[0m\n' "$*"; }
fail() { printf '\033[31m   ✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ── teardown ─────────────────────────────────────────────────────────────────
dump_diagnostics() {
    for c in "$DOCKER_VPS" "$BINARY_VPS"; do
        if docker inspect "$c" >/dev/null 2>&1; then
            echo "── diagnostics: $c ──────────────────────────────" >&2
            docker logs --tail 50 "$c" >&2 || true
            docker exec "$c" journalctl -u e2eapp --no-pager -n 50 >&2 || true
            docker exec "$c" docker ps -a >&2 || true
            # The inner app container's own output is usually the actual
            # failure reason (boot crash, missing asset, bad credentials).
            docker exec "$c" docker logs --tail 60 e2eapp_app >&2 || true
        fi
    done
}

cleanup() {
    status=$?
    if [ "$status" -ne 0 ]; then
        echo "e2e-deploy FAILED (exit $status) — dumping diagnostics" >&2
        dump_diagnostics
    fi
    # -v: also remove the anonymous volumes backing the nested engines.
    docker rm -f -v "$DOCKER_VPS" "$BINARY_VPS" >/dev/null 2>&1 || true
    # Remove per-release app images the deploys built on the HOST daemon.
    docker images 'e2eapp' -q | sort -u | xargs -r docker rmi -f >/dev/null 2>&1 || true
    [ -n "$AGENT_PID" ] && kill "$AGENT_PID" >/dev/null 2>&1 || true
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT

# ── helpers ──────────────────────────────────────────────────────────────────
# run.sh's own assertions ssh in directly with the throwaway key; gofasta's
# ssh child processes authenticate via the ssh-agent started below.
vps_ssh() { # vps_ssh <ssh-port> <command...>
    local port="$1"; shift
    ssh -p "$port" -i "$SSH_KEY" \
        -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR -o ConnectTimeout=10 \
        deploy@127.0.0.1 "$@"
}

wait_for_ssh() { # wait_for_ssh <ssh-port> <name>
    local port="$1" name="$2" i
    for i in $(seq 1 60); do
        if vps_ssh "$port" "echo ok" >/dev/null 2>&1; then
            pass "$name sshd is up"
            return 0
        fi
        sleep 2
    done
    fail "$name: sshd did not come up on port $port"
}

wait_for_health() { # wait_for_health <app-port> <what>
    local port="$1" what="$2" i
    for i in $(seq 1 60); do
        if curl -sf "http://127.0.0.1:$port/health/ready" >/dev/null 2>&1; then
            pass "$what is serving /health/ready on :$port"
            return 0
        fi
        sleep 2
    done
    fail "$what: /health/ready never answered on :$port"
}

start_vps() { # start_vps <name> <ssh-port> <app-port>
    # /var/lib/docker and /var/lib/containerd ride anonymous volumes: the
    # nested Docker engine cannot stack overlayfs on the outer container's
    # overlayfs ("invalid argument" on container start without this).
    docker run -d --name "$1" \
        --privileged --cgroupns=host \
        --tmpfs /run --tmpfs /run/lock \
        -v /var/lib/docker -v /var/lib/containerd \
        -p "127.0.0.1:$2:22" -p "127.0.0.1:$3:8080" \
        "$VPS_IMAGE" >/dev/null
}

current_release() { # current_release <ssh-port>
    vps_ssh "$1" "readlink /opt/e2eapp/current" | xargs -r basename
}

# gofasta deploy invocations share the project cwd; --port selects which VPS.
deploy() { # deploy <ssh-port> [extra args...]
    local port="$1"; shift
    (cd "$PROJECT_DIR" && "$GOFASTA_BIN" deploy --port "$port" "$@")
}

# ReleaseTag has 1-second granularity — consecutive deploys need a beat.
tick() { sleep 2; }

# ═════════════════════════════════════════════════════════════════════════════
log "preconditions"
command -v docker >/dev/null || fail "docker is required"
[ -x "$GOFASTA_BIN" ] || fail "gofasta binary not found at $GOFASTA_BIN (run 'make build')"
SERVER_ARCH="$(docker version --format '{{.Server.Arch}}')"
case "$SERVER_ARCH" in
    amd64|arm64) pass "docker server arch: $SERVER_ARCH" ;;
    *) fail "unsupported docker server arch: $SERVER_ARCH" ;;
esac

log "building fake-VPS image"
ssh-keygen -t ed25519 -N '' -q -f "$SSH_KEY"
docker build -q --build-arg SSH_PUBKEY="$(cat "$SSH_KEY.pub")" \
    -t "$VPS_IMAGE" "$SCRIPT_DIR" >/dev/null
pass "image $VPS_IMAGE built"

# gofasta deploy shells out to plain `ssh`/`scp` with no -i flag; an agent
# holding the throwaway key is how those children authenticate.
eval "$(ssh-agent -s)" >/dev/null
AGENT_PID="$SSH_AGENT_PID"
ssh-add -q "$SSH_KEY"

log "scaffolding project"
"$GOFASTA_BIN" new "$PROJECT_DIR" --driver postgres
cd "$PROJECT_DIR"
[ -f .env ] || cp .env.example .env

ensure_env() { # ensure_env <KEY> <value> — set KEY if empty/missing
    if grep -q "^$1=..*" .env; then return 0; fi
    if grep -q "^$1=" .env; then
        sed -i.bak "s|^$1=.*|$1=$2|" .env && rm -f .env.bak
    else
        printf '%s=%s\n' "$1" "$2" >> .env
    fi
}
ensure_env E2EAPP_AUTH_JWT_SECRET "$(openssl rand -hex 32)"
ensure_env E2EAPP_SESSION_SECRET "$(openssl rand -hex 32)"
ensure_env E2EAPP_DATABASE_USER e2eapp
ensure_env E2EAPP_DATABASE_PASSWORD e2eapp
ensure_env E2EAPP_DATABASE_NAME e2eapp_dev
# Binary method: the app + migrate run on the VPS itself against its local
# PostgreSQL on the standard port. The scaffold's dev .env points at the
# dev compose's remapped host port (5433) — a production .env wouldn't.
# Docker method ignores both (compose pins DATABASE_HOST=db, port 5432).
sed -i.bak \
    -e "s|^E2EAPP_DATABASE_HOST=.*|E2EAPP_DATABASE_HOST=127.0.0.1|" \
    -e "s|^E2EAPP_DATABASE_PORT=.*|E2EAPP_DATABASE_PORT=5432|" \
    .env && rm -f .env.bak
grep -q "^E2EAPP_DATABASE_PORT=5432" .env || printf 'E2EAPP_DATABASE_PORT=5432\n' >> .env

# Point the scaffold's deploy config at the harness. Throwaway host keys
# change on every container rebuild, so host-key pinning is disabled.
sed -i.bak \
    -e 's|host: ""|host: "deploy@127.0.0.1"|' \
    -e 's|path: ""|path: "/opt/e2eapp"|' \
    -e "s|arch: amd64|arch: $SERVER_ARCH|" \
    -e 's|health_timeout: 30|health_timeout: 120|' \
    -e 's|strict_host_key: accept-new|strict_host_key: "no"|' \
    config.yaml && rm -f config.yaml.bak
grep -q 'strict_host_key: "no"' config.yaml || fail "config.yaml is missing the strict_host_key key (template drift?)"

pass "project scaffolded and configured"

# ═════════════════════════════════════════════════════════════════════════════
# Docker method
# ═════════════════════════════════════════════════════════════════════════════
log "docker method: starting VPS"
start_vps "$DOCKER_VPS" "$SSH_PORT_DOCKER" "$APP_PORT_DOCKER"
wait_for_ssh "$SSH_PORT_DOCKER" "$DOCKER_VPS"

log "docker: gofasta deploy setup"
deploy "$SSH_PORT_DOCKER" setup
pass "setup succeeded"

log "docker: first deploy"
deploy "$SSH_PORT_DOCKER"
wait_for_health "$APP_PORT_DOCKER" "first release"
R1="$(current_release "$SSH_PORT_DOCKER")"
[ -n "$R1" ] || fail "current symlink not set after first deploy"
pass "current release: $R1"

log "docker: seeding persistence marker"
vps_ssh "$SSH_PORT_DOCKER" \
    "docker exec e2eapp_db psql -U e2eapp -d e2eapp_dev -c 'CREATE TABLE IF NOT EXISTS e2e_marker(id int); INSERT INTO e2e_marker VALUES (42);'" \
    >/dev/null
pass "marker row written"

log "docker: repeat deploy (container/volume reuse regression gate)"
tick
deploy "$SSH_PORT_DOCKER"
wait_for_health "$APP_PORT_DOCKER" "second release"
R2="$(current_release "$SSH_PORT_DOCKER")"
[ "$R2" != "$R1" ] || fail "current release did not advance on repeat deploy"
MARKER="$(vps_ssh "$SSH_PORT_DOCKER" \
    "docker exec e2eapp_db psql -U e2eapp -d e2eapp_dev -tAc 'SELECT count(*) FROM e2e_marker;'")"
[ "$MARKER" = "1" ] || fail "database volume did not survive the repeat deploy (marker=$MARKER)"
pass "repeat deploy OK — data survived, current: $R2"

log "docker: status (text + --json)"
deploy "$SSH_PORT_DOCKER" status | grep -q "$R2" || fail "status does not show current release"
(cd "$PROJECT_DIR" && "$GOFASTA_BIN" --json deploy status --port "$SSH_PORT_DOCKER") \
    | grep -q '"current_release"' || fail "--json status missing current_release"
pass "status OK"

log "docker: induced-failure deploy must auto-rollback"
cat > cmd/e2e_fail.go <<'EOF'
package cmd

import (
	"fmt"
	"os"
)

// Injected by the deploy e2e harness: makes `serve` die on boot so the
// deploy's health gate fails and automatic rollback is exercised.
func init() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		fmt.Fprintln(os.Stderr, "e2e induced failure")
		os.Exit(1)
	}
}
EOF
tick
set +e
GOFASTA_DEPLOY_HEALTH_TIMEOUT=20 deploy "$SSH_PORT_DOCKER"
DEPLOY_RC=$?
set -e
rm -f cmd/e2e_fail.go
[ "$DEPLOY_RC" -ne 0 ] || fail "deploy of a broken release exited 0"
wait_for_health "$APP_PORT_DOCKER" "previous release (after auto-rollback)"
R_AFTER_FAIL="$(current_release "$SSH_PORT_DOCKER")"
[ "$R_AFTER_FAIL" = "$R2" ] || fail "current moved to the broken release ($R_AFTER_FAIL != $R2)"
pass "broken release rejected, $R2 still live"

log "docker: third deploy + explicit rollback"
tick
deploy "$SSH_PORT_DOCKER"
wait_for_health "$APP_PORT_DOCKER" "third release"
R3="$(current_release "$SSH_PORT_DOCKER")"
[ "$R3" != "$R2" ] || fail "third deploy did not advance current"
deploy "$SSH_PORT_DOCKER" rollback
wait_for_health "$APP_PORT_DOCKER" "rolled-back release"
R_ROLLED="$(current_release "$SSH_PORT_DOCKER")"
[ "$R_ROLLED" = "$R2" ] || fail "rollback landed on $R_ROLLED, expected $R2"
pass "rollback restored $R2"

docker rm -f -v "$DOCKER_VPS" >/dev/null
pass "docker method: all scenarios green"

# ═════════════════════════════════════════════════════════════════════════════
# Binary method — a FRESH server (first-deploy-must-work regression gate)
# ═════════════════════════════════════════════════════════════════════════════
log "binary method: starting fresh VPS"
start_vps "$BINARY_VPS" "$SSH_PORT_BINARY" "$APP_PORT_BINARY"
wait_for_ssh "$SSH_PORT_BINARY" "$BINARY_VPS"

log "binary: provisioning local PostgreSQL"
vps_ssh "$SSH_PORT_BINARY" "sudo systemctl start postgresql && \
    sudo -u postgres psql -tc \"SELECT 1 FROM pg_roles WHERE rolname='e2eapp'\" | grep -q 1 || \
    sudo -u postgres psql -c \"CREATE ROLE e2eapp LOGIN PASSWORD 'e2eapp'\"" >/dev/null
vps_ssh "$SSH_PORT_BINARY" "sudo -u postgres psql -tc \"SELECT 1 FROM pg_database WHERE datname='e2eapp_dev'\" | grep -q 1 || \
    sudo -u postgres createdb -O e2eapp e2eapp_dev" >/dev/null
pass "postgres role + database ready"

log "binary: gofasta deploy setup"
deploy "$SSH_PORT_BINARY" setup --method binary
pass "setup succeeded"

log "binary: first deploy on a fresh server"
deploy "$SSH_PORT_BINARY" --method binary
wait_for_health "$APP_PORT_BINARY" "first binary release"
B1="$(current_release "$SSH_PORT_BINARY")"
pass "current release: $B1"

log "binary: repeat deploy + status"
tick
deploy "$SSH_PORT_BINARY" --method binary
wait_for_health "$APP_PORT_BINARY" "second binary release"
B2="$(current_release "$SSH_PORT_BINARY")"
[ "$B2" != "$B1" ] || fail "current release did not advance on repeat binary deploy"
deploy "$SSH_PORT_BINARY" status --method binary | grep -q "$B2" || fail "binary status does not show current release"
pass "repeat deploy + status OK"

log "binary: explicit rollback"
deploy "$SSH_PORT_BINARY" rollback --method binary
wait_for_health "$APP_PORT_BINARY" "rolled-back binary release"
B_ROLLED="$(current_release "$SSH_PORT_BINARY")"
[ "$B_ROLLED" = "$B1" ] || fail "binary rollback landed on $B_ROLLED, expected $B1"
pass "rollback restored $B1"

log "ALL e2e deploy scenarios green"
