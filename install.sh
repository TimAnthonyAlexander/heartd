#!/usr/bin/env bash
#
# install.sh — install heartd as a systemd service on Linux.
#
# What it does (each step is privileged via sudo when you aren't root):
#   1. Installs a heartd binary to /usr/local/bin/heartd
#   2. Creates a dedicated, unprivileged `heartd` system user + group
#   3. Creates /etc/heartd (config) and /var/lib/heartd (database)
#   4. Writes /etc/heartd/heartd.yaml (only if absent — never clobbers yours)
#   5. Installs and starts a hardened systemd unit bound to 127.0.0.1
#   6. Prints an nginx reverse-proxy block for you to add (does NOT touch nginx)
#   7. Optionally adds an nginx firewall rule — but ONLY after asking
#
# It does NOT configure nginx or TLS, and never opens port 9300 publicly:
# heartd listens on localhost and is meant to sit behind your own reverse proxy.
#
# Usage:
#   sudo ./install.sh [options]
#
# Options:
#   --binary PATH     Path to a linux heartd binary to install. If omitted, the
#                     script looks for ./bin/heartd-linux-<arch> then ./heartd.
#   --name NAME       Node name shown in the dashboard (default: this hostname).
#   --domain HOST     Public hostname (e.g. heartd.example.com). Used for the
#                     printed nginx block and as advertise_url in a fresh config.
#   --port PORT       Localhost port heartd binds to (default: 9300).
#   --force-config    Overwrite an existing /etc/heartd/heartd.yaml (a timestamped
#                     backup is kept).
#   --no-start        Install everything but don't enable/start the service.
#   --yes             Assume "yes" for prompts (non-interactive). Still does NOT
#                     change the firewall unless combined with --ufw.
#   --ufw             Allow the firewall step to run (still prints what it does).
#   -h, --help        Show this help and exit.

set -euo pipefail

# ----- defaults -----
BINARY=""
NODE_NAME=""
DOMAIN=""
PORT="9300"
FORCE_CONFIG="no"
DO_START="yes"
ASSUME_YES="no"
ALLOW_UFW="no"

PREFIX_BIN="/usr/local/bin/heartd"
CONFIG_DIR="/etc/heartd"
CONFIG_FILE="${CONFIG_DIR}/heartd.yaml"
DATA_DIR="/var/lib/heartd"
UNIT_FILE="/etc/systemd/system/heartd.service"
SVC_USER="heartd"
SVC_GROUP="heartd"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

# ----- pretty output -----
c_blue() { printf '\033[1;34m%s\033[0m\n' "$*"; }
c_green() { printf '\033[1;32m%s\033[0m\n' "$*"; }
c_yellow() { printf '\033[1;33m%s\033[0m\n' "$*"; }
c_red() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; }
info() { printf '  %s\n' "$*"; }
die() {
  c_red "error: $*"
  exit 1
}

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; s/^#//' | sed '$d'
  exit "${1:-0}"
}

# ----- parse args -----
while [ $# -gt 0 ]; do
  case "$1" in
  --binary)
    BINARY="${2:-}"
    shift 2
    ;;
  --name)
    NODE_NAME="${2:-}"
    shift 2
    ;;
  --domain)
    DOMAIN="${2:-}"
    shift 2
    ;;
  --port)
    PORT="${2:-}"
    shift 2
    ;;
  --force-config)
    FORCE_CONFIG="yes"
    shift
    ;;
  --no-start)
    DO_START="no"
    shift
    ;;
  --yes)
    ASSUME_YES="yes"
    shift
    ;;
  --ufw)
    ALLOW_UFW="yes"
    shift
    ;;
  -h | --help) usage 0 ;;
  *) die "unknown option: $1 (try --help)" ;;
  esac
done

# ----- prompt helper (defaults to NO; honours --yes; safe when non-interactive) -----
confirm() {
  local prompt="$1"
  if [ "$ASSUME_YES" = "yes" ]; then return 0; fi
  if [ ! -t 0 ]; then return 1; fi # no TTY -> treat as "no"
  local reply
  read -r -p "$prompt [y/N] " reply || true
  case "$reply" in [yY] | [yY][eE][sS]) return 0 ;; *) return 1 ;; esac
}

# ----- preflight -----
[ "$(uname -s)" = "Linux" ] || die "this installer is for Linux only (you're on $(uname -s))."
[ -d /run/systemd/system ] || die "systemd not detected; this installer requires systemd."

# Privilege escalation: use sudo when not already root.
if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  command -v sudo >/dev/null 2>&1 || die "not root and sudo not found. Re-run as root."
  SUDO="sudo"
  c_yellow "Not running as root — privileged steps will use sudo (you may be prompted)."
fi

# Detect CPU arch and map to Go's naming.
case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*) die "unsupported architecture: $(uname -m) (heartd ships amd64 and arm64)." ;;
esac

# Resolve the binary to install.
if [ -z "$BINARY" ]; then
  if [ -x "${SCRIPT_DIR}/bin/heartd-linux-${ARCH}" ]; then
    BINARY="${SCRIPT_DIR}/bin/heartd-linux-${ARCH}"
  elif [ -x "${SCRIPT_DIR}/heartd" ]; then
    BINARY="${SCRIPT_DIR}/heartd"
  else
    die "no binary found. Build one with 'make cross' and pass --binary bin/heartd-linux-${ARCH}, or place a 'heartd' binary next to this script."
  fi
fi
[ -f "$BINARY" ] || die "binary not found: $BINARY"

# Sanity-check it's a Linux ELF so we fail loudly on a macOS/Windows binary.
# Read the 4-byte magic as hex (locale-safe; no tr on raw bytes): 7f 45 4c 46.
ELF_MAGIC="$(LC_ALL=C head -c 4 "$BINARY" | od -An -tx1 | tr -d ' \n')"
if [ "$ELF_MAGIC" != "7f454c46" ]; then
  die "$BINARY is not a Linux ELF binary (magic: ${ELF_MAGIC:-empty}). Use the linux/${ARCH} build (make cross)."
fi

NODE_NAME="${NODE_NAME:-$(hostname)}"

c_blue "heartd installer"
info "binary:    $BINARY"
info "arch:      $ARCH"
info "node name: $NODE_NAME"
info "bind:      127.0.0.1:${PORT}  (localhost only — reverse proxy in front of it)"
info "config:    $CONFIG_FILE"
info "database:  ${DATA_DIR}/heartd.db"
echo

# ----- 1. install binary -----
c_blue "1/6  Installing binary -> ${PREFIX_BIN}"
$SUDO install -m 0755 "$BINARY" "$PREFIX_BIN"

# ----- 2. system user + group -----
c_blue "2/6  Creating ${SVC_USER} system user"
if ! getent group "$SVC_GROUP" >/dev/null 2>&1; then
  $SUDO groupadd --system "$SVC_GROUP"
  info "created group $SVC_GROUP"
else
  info "group $SVC_GROUP already exists"
fi
if ! id "$SVC_USER" >/dev/null 2>&1; then
  $SUDO useradd --system --gid "$SVC_GROUP" --no-create-home \
    --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$SVC_USER"
  info "created user $SVC_USER"
else
  info "user $SVC_USER already exists"
fi

# ----- 3. directories -----
c_blue "3/6  Creating directories"
$SUDO mkdir -p "$CONFIG_DIR" "$DATA_DIR"
$SUDO chown "${SVC_USER}:${SVC_GROUP}" "$DATA_DIR"
$SUDO chmod 0750 "$DATA_DIR"

# ----- 4. config (never clobber an existing one unless --force-config) -----
c_blue "4/6  Writing config"
ADVERTISE_LINE="# advertise_url:                       # set to https://<your-domain> once peers exist"
if [ -n "$DOMAIN" ]; then
  ADVERTISE_LINE="advertise_url: https://${DOMAIN}"
fi

write_config() {
  $SUDO tee "$CONFIG_FILE" >/dev/null <<YAML
# heartd configuration — generated by install.sh.
# Most operational settings (checks, notifications, thresholds, intervals) are
# editable live from the dashboard; this file only seeds them on first run.
# Durations accept Go units plus "d" (days): 30s, 5m, 1h, 7d.

server:
  name: ${NODE_NAME}
  metrics_interval: 30s
  retention: 30d
  db_path: ${DATA_DIR}/heartd.db
  peer_poll_interval: 15s
  ${ADVERTISE_LINE}

# System-metric alert thresholds (percent). 0 disables a metric.
thresholds:
  cpu_percent: 90
  mem_percent: 90
  disk_percent: 90

# Add peers / checks / notify here, or (preferred) from the dashboard tabs.
YAML
  $SUDO chown "root:${SVC_GROUP}" "$CONFIG_FILE"
  $SUDO chmod 0640 "$CONFIG_FILE"
}

if [ -f "$CONFIG_FILE" ] && [ "$FORCE_CONFIG" != "yes" ]; then
  info "$CONFIG_FILE already exists — keeping it (use --force-config to replace)."
elif [ -f "$CONFIG_FILE" ]; then
  BACKUP="${CONFIG_FILE}.bak.$(date +%Y%m%d%H%M%S)"
  $SUDO cp -a "$CONFIG_FILE" "$BACKUP"
  info "backed up existing config to $BACKUP"
  write_config
  info "wrote fresh $CONFIG_FILE"
else
  write_config
  info "wrote $CONFIG_FILE"
fi

# ----- 5. systemd unit -----
c_blue "5/6  Installing systemd service"
$SUDO tee "$UNIT_FILE" >/dev/null <<UNIT
[Unit]
Description=heartd server health monitor
Documentation=https://github.com/timanthonyalexander/heartd
After=network-online.target
Wants=network-online.target

[Service]
User=${SVC_USER}
Group=${SVC_GROUP}
ExecStart=${PREFIX_BIN} -config ${CONFIG_FILE} -addr 127.0.0.1:${PORT}
Restart=on-failure
RestartSec=2

# Sandboxing. NOTE: ProtectSystem=strict makes the FS read-only except the paths
# below. http/tcp/process metric checks work fine; a 'shell' check that writes or
# reads a protected path needs its path added to ReadWritePaths/ReadOnlyPaths.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=${DATA_DIR}
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
UNIT

$SUDO systemctl daemon-reload

if [ "$DO_START" = "yes" ]; then
  $SUDO systemctl enable --now heartd
  info "enabled and started heartd.service"

  # Brief health check.
  if command -v curl >/dev/null 2>&1; then
    ok="no"
    for _ in 1 2 3 4 5; do
      if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
        ok="yes"
        break
      fi
      sleep 1
    done
    if [ "$ok" = "yes" ]; then
      c_green "heartd is responding on http://127.0.0.1:${PORT}"
    else
      c_yellow "heartd did not answer /api/health yet — check: journalctl -u heartd -e"
    fi
  fi
else
  info "skipping start (--no-start). Start later with: $SUDO systemctl enable --now heartd"
fi

# ----- 6. nginx instructions (printed, never applied) -----
c_blue "6/6  Reverse proxy (manual step — nothing was changed)"
NGINX_HOST="${DOMAIN:-heartd.example.com}"
cat <<NGINX

heartd serves plain HTTP on 127.0.0.1:${PORT}. Put it behind your existing nginx
and terminate TLS there. Add a server block like this:

  # /etc/nginx/sites-available/heartd
  server {
      listen 80;
      listen [::]:80;
      server_name ${NGINX_HOST};

      location / {
          proxy_pass http://127.0.0.1:${PORT};
          proxy_http_version 1.1;
          proxy_set_header Host              \$host;
          proxy_set_header X-Real-IP         \$remote_addr;
          proxy_set_header X-Forwarded-For   \$proxy_add_x_forwarded_for;
          proxy_set_header X-Forwarded-Proto \$scheme;
          proxy_read_timeout 30s;
      }
  }

Then enable it and add TLS (no websocket config needed):

  sudo ln -s /etc/nginx/sites-available/heartd /etc/nginx/sites-enabled/heartd
  sudo nginx -t && sudo systemctl reload nginx
  sudo certbot --nginx -d ${NGINX_HOST}

NGINX

# ----- optional firewall step (asks first) -----
if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -q "Status: active"; then
  c_yellow "ufw is active. heartd itself needs NO open port (it's localhost-only)."
  info "Public access goes through nginx on 80/443, which you may already allow."
  if [ "$ALLOW_UFW" = "yes" ] || confirm "Allow nginx (80,443) through ufw now? ('Nginx Full')"; then
    $SUDO ufw allow 'Nginx Full'
    c_green "added ufw rule 'Nginx Full' (80,443). Port ${PORT} remains closed."
  else
    info "left firewall unchanged. To allow nginx later: sudo ufw allow 'Nginx Full'"
  fi
fi

echo
c_green "Done."
info "Next: point your nginx block above at this host, get a cert, then open"
info "  https://${NGINX_HOST}/  and create the first admin account."
info "Logs:    journalctl -u heartd -f"
info "Config:  $CONFIG_FILE   (restart after edits: $SUDO systemctl restart heartd)"
