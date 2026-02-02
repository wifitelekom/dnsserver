#!/usr/bin/env bash
set -euo pipefail

# ========= Configurable defaults =========
CLICKHOUSE_HTTP="127.0.0.1:8123"
DNSTAP_SOCK="/run/dnsdist/dnstap.sock"
COLLECTOR_BIN="/usr/local/bin/dnsdist-collector"
DASHBOARD_DIR="/opt/dns-dashboard"
DNSDIST_CONF_SRC="./dnsdist/dnsdist.conf"
DNSDIST_CONF_DST="/etc/dnsdist/dnsdist.conf"
UNBOUND_CONF_DIR="/etc/unbound/unbound.conf.d"
# =========================================

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "Run as root (sudo -i)."
    exit 1
  fi
}

log() { echo -e "\n==> $*\n"; }

ensure_tools() {
  log "Installing base tools"
  apt-get update
  apt-get install -y --no-install-recommends \
    ca-certificates curl gnupg lsb-release jq \
    build-essential git pkg-config
}

install_clickhouse_repo() {
  log "Installing ClickHouse official repository"

  sudo mkdir -p /etc/apt/keyrings
  sudo rm -f /etc/apt/keyrings/clickhouse.gpg

  curl -fsSL https://packages.clickhouse.com/keys/clickhouse.gpg \
    | sudo gpg --dearmor -o /etc/apt/keyrings/clickhouse.gpg

  ARCH="$(dpkg --print-architecture)"

  echo "deb [signed-by=/etc/apt/keyrings/clickhouse.gpg arch=${ARCH}] https://packages.clickhouse.com/deb stable main" \
    | sudo tee /etc/apt/sources.list.d/clickhouse.list >/dev/null
  apt-get update
}

install_packages() {
  log "Installing dnsdist, unbound, clickhouse"
  # dnsdist + unbound from Ubuntu repo
  apt-get install -y --no-install-recommends dnsdist unbound

  # clickhouse from official repo
  apt-get install -y --no-install-recommends clickhouse-server clickhouse-client

  systemctl enable --now clickhouse-server
  systemctl enable --now unbound
  systemctl enable --now dnsdist
}

deploy_unbound_conf() {
  log "Deploying Unbound configuration"
  mkdir -p "${UNBOUND_CONF_DIR}"

  # Copy our drop-in conf files
  install -m 0644 ./unbound/unbound.conf.d/00-base.conf "${UNBOUND_CONF_DIR}/00-base.conf"
  install -m 0644 ./unbound/unbound.conf.d/10-recursive.conf "${UNBOUND_CONF_DIR}/10-recursive.conf"
  install -m 0644 ./unbound/unbound.conf.d/90-listen.conf "${UNBOUND_CONF_DIR}/90-listen.conf"

  # Validate
  unbound-checkconf

  systemctl restart unbound
  systemctl --no-pager -l status unbound || true
}

deploy_dnsdist_conf() {
  log "Deploying dnsdist configuration"
  install -m 0644 "${DNSDIST_CONF_SRC}" "${DNSDIST_CONF_DST}"
  install -m 0644 ./dnsdist/allowlist.txt /etc/dnsdist/allowlist.txt
  install -m 0644 ./dnsdist/blocklist.txt /etc/dnsdist/blocklist.txt

  # Validate config
  dnsdist -C "${DNSDIST_CONF_DST}" --check-config

  systemctl restart dnsdist
  systemctl --no-pager -l status dnsdist || true
}

build_collector() {
  log "Building dnsdist-collector (Go)"
  apt-get install -y --no-install-recommends golang

  pushd ./collector >/dev/null
  go mod tidy
  go build -o /tmp/dnsdist-collector .
  popd >/dev/null

  install -m 0755 /tmp/dnsdist-collector "${COLLECTOR_BIN}"
}

build_dashboard() {
  log "Building dns-dashboard (Go)"
  # Ensure we have go
  apt-get install -y --no-install-recommends golang

  pushd ./dns-dashboard >/dev/null
  go mod tidy
  go build -o /tmp/dns-dashboard .
  popd >/dev/null

  mkdir -p "${DASHBOARD_DIR}/views"
  install -m 0755 /tmp/dns-dashboard "${DASHBOARD_DIR}/dns-dashboard"
  cp -r ./dns-dashboard/views/* "${DASHBOARD_DIR}/views/"
  if [ -d "./dns-dashboard/static" ]; then
     mkdir -p "${DASHBOARD_DIR}/static"
     cp -r ./dns-dashboard/static/* "${DASHBOARD_DIR}/static/"
  fi
}

deploy_systemd() {
  log "Deploying systemd unit + tmpfiles"
  install -m 0644 ./systemd/dnsdist-collector.service /etc/systemd/system/dnsdist-collector.service
  install -m 0644 ./systemd/dnsdist-collector.tmpfiles.conf /etc/tmpfiles.d/dnsdist-collector.conf

  systemd-tmpfiles --create /etc/tmpfiles.d/dnsdist-collector.conf

  # Dashboard Service
  install -m 0644 ./systemd/dns-dashboard.service /etc/systemd/system/dns-dashboard.service

  systemctl daemon-reload
  systemctl enable --now dnsdist-collector
  systemctl enable --now dns-dashboard
  systemctl --no-pager -l status dnsdist-collector || true
  systemctl --no-pager -l status dns-dashboard || true
}

init_clickhouse_schema() {
  log "Creating ClickHouse database/table schema"
  clickhouse-client --query "CREATE DATABASE IF NOT EXISTS dns"
  clickhouse-client --multiquery < ./clickhouse/schema.sql
}

health_checks() {
  log "Health checks"
  echo "[1] ClickHouse version:"
  clickhouse-client --query "SELECT version()"

  echo "[2] dnsdist listening:"
  ss -lunp | egrep ':53\b' || true

  echo "[3] Unbound listening:"
  ss -lunp | egrep ':5353\b' || true

  echo "[4] dnstap unix socket:"
  ls -la "${DNSTAP_SOCK}" || true

  echo "[5] ClickHouse HTTP endpoint:"
  curl -s "http://${CLICKHOUSE_HTTP}/?query=SELECT%201" || true

  echo "[6] Collector status:"
  systemctl --no-pager -l status dnsdist-collector || true
}

main() {
  require_root
  ensure_tools
  install_clickhouse_repo
  install_packages
  init_clickhouse_schema
  deploy_unbound_conf
  deploy_dnsdist_conf
  build_collector
  build_dashboard
  deploy_systemd
  health_checks

  log "Done. Next: run a dig test and check ClickHouse."
  echo "Example:"
  echo "  dig @127.0.0.1 google.com +short"
  echo "  clickhouse-client --query \"SELECT timestamp, client_ip, qname, qtype, response_type, rcode FROM dns.dns_logs ORDER BY timestamp DESC LIMIT 20\""
}

main "$@"
