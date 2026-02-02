# Wifi Telekom DNS Server (dnsdist + Unbound + dnstap + ClickHouse)

## Mimari
Client → dnsdist (53/udp+tcp, cache, gerçek client IP) → Unbound (127.0.0.1:5353 recursive)  
dnsdist → dnstap (FrameStream unix socket /run/dnsdist/dnstap.sock) → dnsdist-collector (Go) → ClickHouse

## Gereksinimler
- Ubuntu 24.04
- Root erişimi
- İnternet erişimi (ClickHouse repo, go modules)

## Kurulum
Repo kök dizininde:

```bash
sudo -i
cd /opt/dnsserver   # örnek
bash ./install.sh




1) Servisler
systemctl status clickhouse-server --no-pager -l
systemctl status unbound --no-pager -l
systemctl status dnsdist --no-pager -l
systemctl status dnsdist-collector --no-pager -l

2) DNS test
dig @127.0.0.1 google.com +short
dig @127.0.0.1 nonexistent-xyz12345.example +short

3) ClickHouse log kontrol
clickhouse-client --query "SELECT timestamp, client_ip, qname, qtype, response_type, rcode FROM dns.dns_logs ORDER BY timestamp DESC LIMIT 20"

Troubleshooting
Collector insert atmıyor

Collector HTTP portu: 8123 olmalı.

curl -s 'http://127.0.0.1:8123/?query=SELECT%201'

dnstap socket yok

dnsdist config’te socket path doğru mu?

collector önce kalkarsa socket’i oluşturur, dnsdist bağlanır. (İkisi de çalışmalı)

ls -la /run/dnsdist/dnstap.sock
ss -xlpn | grep dnstap.sock

Performans notları

dnsdist PacketCache aktif

Unbound local-only

dnstap logging async; collector drop/backpressure mekanizması kodda var (buffer dolarsa drop)

GitHub

Bu repo:

install.sh ile clean install

dnsdist/, unbound/, systemd/, clickhouse/ ile tüm configler

collector/ altında Go collector kaynağı içerir


---

# 7) “GitHub’a yükleyelim mi?”
Evet, bence kesin yükleyelim. Çünkü:
- Versiyon kontrollü config (dnsdist/unbound/systemd)
- Kurulum scripti
- Collector kaynak kodu
- Üretim değişiklikleri (whitelist, NXDOMAIN stratejisi, batch tuning) PR ile gider

## Push adımları
Sunucuda/PC’de repo dizininde:

```bash
git init
git remote add origin https://github.com/wifitelekom/dnsserver.git

git add .
git commit -m "Initial clean install: dnsdist+unbound+dnstap collector+clickhouse"
git branch -M main
git push -u origin main