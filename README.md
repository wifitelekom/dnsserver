# Wifi Telekom DNS Server (dnsdist + Unbound + dnstap + ClickHouse)

## Architecture
Client -> dnsdist (53/udp+tcp, cache, real client IP) -> Unbound (127.0.0.1:5353 recursive)
dnsdist -> dnstap (FrameStream unix socket /run/dnsdist/dnstap.sock) -> dnsdist-collector (Go) -> ClickHouse -> Dashboard

## Requirements
- Ubuntu 24.04
- Root access
- Internet access (ClickHouse repo, Go modules)

## Install
From repo root:

```bash
sudo -i
cd /opt/dnsserver   # example
bash ./install.sh
```

## Services
```bash
systemctl status clickhouse-server --no-pager -l
systemctl status unbound --no-pager -l
systemctl status dnsdist --no-pager -l
systemctl status dnsdist-collector --no-pager -l
systemctl status dns-dashboard --no-pager -l
```

## DNS Test
```bash
dig @127.0.0.1 google.com +short
dig @127.0.0.1 nonexistent-xyz12345.example +short
```

## ClickHouse Check
```bash
clickhouse-client --query "SELECT timestamp, client_ip, qname, qtype, response_type, rcode FROM dns.dns_logs ORDER BY timestamp DESC LIMIT 20"
```

## Dashboard Auth
Basic auth is required. Set credentials in `systemd/dns-dashboard.service`:

```
Environment="DASHBOARD_USER=admin"
Environment="DASHBOARD_PASS=change_me"
```

## Blocklist / Allowlist
Update these files and restart `dnsdist`:
- `/etc/dnsdist/blocklist.txt`
- `/etc/dnsdist/allowlist.txt`

Format: one domain per line. Suffix/wildcard supported (`example.com`, `*.example.com`, `.example.com`).

## Retention
ClickHouse table TTL is 7 days (logs expire automatically).

## Troubleshooting
- Collector not inserting
  - ClickHouse HTTP port should be 8123.
  - `curl -s 'http://127.0.0.1:8123/?query=SELECT%201'`
- dnstap socket missing
  - Check dnsdist config path and service order.
  - `ls -la /run/dnsdist/dnstap.sock`
  - `ss -xlpn | grep dnstap.sock`

## Repo Layout
- `dnsdist/` dnsdist config
- `unbound/` unbound config
- `systemd/` systemd service units and tmpfiles
- `clickhouse/` schema
- `collector/` Go collector
- `dns-dashboard/` Go dashboard
