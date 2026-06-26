#!/usr/bin/env bash
#
# install.sh — install heartd as a systemd service on Linux.
#
# One-liner (downloads the latest release binary for your CPU, no clone needed):
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/install.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/install.sh | sudo bash -s -- --domain heartd.example.com
#
# What it does (each step is privileged via sudo when you aren't root):
#   1. Installs a heartd binary to /usr/local/bin/heartd (a local one if present,
#      otherwise downloaded from GitHub Releases for your architecture)
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
#                     script looks for ./bin/heartd-linux-<arch> then ./heartd,
#                     and finally downloads it from GitHub Releases.
#   --version TAG     Release tag to download (default: latest). E.g. v1.2.0.
#   --repo OWNER/REPO GitHub repo to download from (default: timanthonyalexander/heartd).
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
#   --headless        Install as a headless AGENT: no dashboard/nginx/TLS. Binds
#                     0.0.0.0:<port>, serves only health + the secret-protected
#                     peer API, and prints how to add it on your HQ node.
#   --secret SECRET   Shared secret for the peer link (headless mode). If omitted
#                     in --headless, one is generated and printed.
#   --bind HOST       Bind address override: 0.0.0.0 (reachable directly, plain
#                     HTTP) or 127.0.0.1 (local; you front it with your own TLS).
#                     Headless installs ask if this is not given.
#   -h, --help        Show this help and exit.
#
# Headless agent one-liner (public box you just want HQ to watch):
#   curl -fsSL https://raw.githubusercontent.com/timanthonyalexander/heartd/main/install.sh | sudo bash -s -- --headless --secret <SHARED_SECRET>

set -euo pipefail

# ----- defaults -----
BINARY=""
VERSION="latest"
REPO="timanthonyalexander/heartd"
NODE_NAME=""
DOMAIN=""
PORT="9300"
FORCE_CONFIG="no"
DO_START="yes"
ASSUME_YES="no"
ALLOW_UFW="no"
HEADLESS="no"
SECRET=""
BIND_HOST_OVERRIDE="" # 127.0.0.1 or 0.0.0.0; empty = ask (headless) / default
DOWNLOADED="" # temp file to clean up if we downloaded the binary

PREFIX_BIN="/usr/local/bin/heartd"
CONFIG_DIR="/etc/heartd"
CONFIG_FILE="${CONFIG_DIR}/heartd.yaml"
DATA_DIR="/var/lib/heartd"
UNIT_FILE="/etc/systemd/system/heartd.service"
SVC_USER="heartd"
SVC_GROUP="heartd"

# Resolve our own directory, tolerating `curl | bash` (no script file on disk).
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo .)"
trap 'rm -f "${DOWNLOADED:-}"' EXIT

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
  --version)
    VERSION="${2:-}"
    shift 2
    ;;
  --repo)
    REPO="${2:-}"
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
  --headless)
    HEADLESS="yes"
    shift
    ;;
  --secret)
    SECRET="${2:-}"
    shift 2
    ;;
  --bind)
    BIND_HOST_OVERRIDE="${2:-}"
    shift 2
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

# download_binary fetches the release asset for this architecture into a temp
# file and points BINARY at it. Used when no local binary is available (e.g. the
# `curl | bash` one-liner).
download_binary() {
  local asset="heartd-linux-${ARCH}" url tmp
  if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
  else
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
  fi

  tmp="$(mktemp)" || die "could not create a temp file"
  DOWNLOADED="$tmp"
  c_blue "Downloading ${asset} (${VERSION:-latest})"
  info "$url"
  if command -v curl >/dev/null 2>&1; then
    curl -fSL --proto '=https' --tlsv1.2 -o "$tmp" "$url" ||
      die "download failed. Is there a published '${asset}' asset for release '${VERSION}' at github.com/${REPO}? Otherwise build locally (make cross) and pass --binary."
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$tmp" "$url" ||
      die "download failed. Is there a published '${asset}' asset for release '${VERSION}' at github.com/${REPO}?"
  else
    die "need curl or wget to download the binary (or pass --binary PATH)."
  fi
  chmod +x "$tmp"
  BINARY="$tmp"
}

# Resolve the binary to install: explicit --binary, then a local build, then a
# download from GitHub Releases.
if [ -z "$BINARY" ]; then
  if [ -x "${SCRIPT_DIR}/bin/heartd-linux-${ARCH}" ]; then
    BINARY="${SCRIPT_DIR}/bin/heartd-linux-${ARCH}"
  elif [ -x "${SCRIPT_DIR}/heartd" ]; then
    BINARY="${SCRIPT_DIR}/heartd"
  else
    download_binary
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

# gen_secret prints a random hex secret (openssl, else /dev/urandom).
gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    LC_ALL=C head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

# detect_ip best-effort: public IP, else first local IPv4, else a placeholder.
detect_ip() {
  local ip=""
  if command -v curl >/dev/null 2>&1; then
    ip="$(curl -fsS --max-time 4 https://api.ipify.org 2>/dev/null || true)"
  fi
  [ -z "$ip" ] && ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  [ -z "$ip" ] && ip="<this-server-ip>"
  echo "$ip"
}

# In headless mode, ensure we have a shared secret (generate one if not given).
if [ "$HEADLESS" = "yes" ] && [ -z "$SECRET" ]; then
  SECRET="$(gen_secret)"
  SECRET_GENERATED="yes"
fi

# Bind address. Dashboard nodes always bind localhost (behind a reverse proxy).
# Headless agents choose: expose directly over plain HTTP (0.0.0.0), or bind
# localhost and front it yourself with nginx/TLS. --bind overrides; otherwise we
# ask interactively and default to direct/HTTP when piped (non-interactive).
if [ -n "$BIND_HOST_OVERRIDE" ]; then
  BIND_HOST="$BIND_HOST_OVERRIDE"
elif [ "$HEADLESS" = "yes" ]; then
  if [ -t 0 ] && [ "$ASSUME_YES" != "yes" ]; then
    c_blue "How should this agent be reached by your HQ?"
    info "  • direct  — bind 0.0.0.0, plain HTTP on IP:${PORT} (simplest, no certs)"
    info "  • behind  — bind 127.0.0.1, you front it with your own nginx/Caddy + TLS (https)"
    if confirm "Expose directly over plain HTTP? (No = bind 127.0.0.1 for your own TLS)"; then
      BIND_HOST="0.0.0.0"
    else
      BIND_HOST="127.0.0.1"
    fi
  else
    BIND_HOST="0.0.0.0" # non-interactive headless default: direct/HTTP (pass --bind to change)
  fi
else
  BIND_HOST="127.0.0.1"
fi
BIND="${BIND_HOST}:${PORT}"

c_blue "heartd installer"
info "binary:    $BINARY"
info "arch:      $ARCH"
info "node name: $NODE_NAME"
if [ "$HEADLESS" = "yes" ]; then
  info "mode:      headless agent (no dashboard; managed from your HQ)"
  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    info "bind:      ${BIND}  (localhost — front it with your own nginx/TLS)"
  else
    info "bind:      ${BIND}  (reachable directly by your HQ over HTTP)"
  fi
else
  info "mode:      dashboard (behind your reverse proxy)"
  info "bind:      ${BIND}  (localhost only)"
fi
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
  if [ "$HEADLESS" = "yes" ]; then
    $SUDO tee "$CONFIG_FILE" >/dev/null <<YAML
# heartd HEADLESS AGENT config — generated by install.sh.
# This node has no dashboard. It exposes only /api/health and the
# secret-protected peer API, and is configured remotely from your HQ node.

server:
  name: ${NODE_NAME}
  headless: true
  peer_secret: ${SECRET}     # the HQ must use this same secret when adding this node
  metrics_interval: 30s
  retention: 30d
  db_path: ${DATA_DIR}/heartd.db

# Seeds the default CPU/memory/disk alert rules on first run. Configure checks,
# notifications, and alerts for this node from your HQ's dashboard.
thresholds:
  cpu_percent: 90
  mem_percent: 90
  disk_percent: 90
YAML
  else
    $SUDO tee "$CONFIG_FILE" >/dev/null <<YAML
# heartd configuration — generated by install.sh.
# Most operational settings (checks, notifications, alerts, intervals) are
# editable live from the dashboard; this file only seeds them on first run.
# Durations accept Go units plus "d" (days): 30s, 5m, 1h, 7d.

server:
  name: ${NODE_NAME}
  metrics_interval: 30s
  retention: 30d
  db_path: ${DATA_DIR}/heartd.db
  peer_poll_interval: 15s
  ${ADVERTISE_LINE}

# Seeds the default CPU/memory/disk alert rules on first run (percent; 0 skips a
# metric). Edit alerts live on each node's Alerts tab thereafter.
thresholds:
  cpu_percent: 90
  mem_percent: 90
  disk_percent: 90

# Add peers / checks / notify here, or (preferred) from the dashboard tabs.
YAML
  fi
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
ExecStart=${PREFIX_BIN} -config ${CONFIG_FILE} -addr ${BIND}
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

# ----- Headless agent: print how to add it on the HQ, then finish -----
if [ "$HEADLESS" = "yes" ]; then
  echo
  c_blue "Headless agent ready — add it on your HQ"

  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    # Behind-your-own-TLS path: bound to localhost, user fronts it with a proxy.
    cat <<TXT

This node is bound to 127.0.0.1:${PORT} and has no dashboard. Front it with your
own reverse proxy + TLS, then add it on your HQ with the HTTPS URL.

Caddy (auto-HTTPS) — /etc/caddy/Caddyfile:

  agent.example.com {
      reverse_proxy 127.0.0.1:${PORT}
  }

…or nginx: proxy_pass http://127.0.0.1:${PORT};  then  certbot --nginx -d agent.example.com

On your HQ's dashboard, click "+ Add node":

    name:    ${NODE_NAME}
    URL:     https://agent.example.com
    secret:  ${SECRET}

TXT
  else
    # Direct/plain-HTTP path: reachable on IP:port.
    AGENT_IP="$(detect_ip)"
    cat <<TXT

This node has no dashboard. On your HQ node's dashboard, click "+ Add node":

    name:    ${NODE_NAME}
    URL:     http://${AGENT_IP}:${PORT}
    secret:  ${SECRET}

TXT
  fi

  cat <<TXT
Then configure its checks, alerts, and notifications from the HQ — those edits
proxy to this node over the peer link.

TXT

  if [ "${SECRET_GENERATED:-no}" = "yes" ]; then
    c_yellow "A shared secret was generated. Use the SAME secret on every agent and"
    c_yellow "when adding them on the HQ. It won't be shown again:"
    info "  ${SECRET}"
    echo
  fi
  if [ "$BIND_HOST" = "127.0.0.1" ]; then
    info "Your reverse proxy gives you TLS, so the peer link is encrypted."
  else
    c_yellow "Heads up: the peer API is plain HTTP. On a public network the secret and"
    info "metrics travel unencrypted — prefer a private network / VPN, or use --bind 127.0.0.1"
    info "with your own TLS proxy. Make sure port ${PORT} is reachable from your HQ."
  fi
  echo
  c_green "Done."
  info "Logs:   journalctl -u heartd -f"
  info "Config: $CONFIG_FILE   (restart after edits: $SUDO systemctl restart heartd)"
  exit 0
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
