#!/usr/bin/env bash
# Runs ON the demo VM as root, from the folder scripts/demo-org-server-deploy.sh
# copied over: installs Caddy once, writes the server's config (keeping its
# access token across deploys, so a redeploy never signs anybody out), swaps
# the binary and restarts the service.
set -euo pipefail
host=$1 client_id=$2 here=$(cd "$(dirname "$0")" && pwd)

if ! command -v caddy >/dev/null; then
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq caddy
fi
id codeaf >/dev/null 2>&1 || useradd --system --home-dir /var/lib/codeaf --shell /usr/sbin/nologin codeaf
install -d -o codeaf -g codeaf -m 700 /var/lib/codeaf /var/lib/codeaf/home /var/lib/codeaf/home/factory /var/lib/codeaf/user

config=/var/lib/codeaf/home/factory/config.yaml
token=""
[ -f "$config" ] && token=$(sed -n 's/^access_token: *"\{0,1\}\([0-9a-f]*\)"\{0,1\}$/\1/p' "$config")
# tls is off because Caddy terminates HTTPS; domain makes every link the
# server prints (operator link, join links) name the public https address.
cat > "$config.new" <<CFG
mode: team
name: codeaf teams demo
listen: 127.0.0.1:8787
access_token: "$token"
tls: "off"
domain: $host
github_client_id: $client_id
trusted_proxies: ["127.0.0.1/32"]
CFG
chown codeaf:codeaf "$config.new"; chmod 600 "$config.new"; mv "$config.new" "$config"

# Never copy over a binary that may be running: remove it, then install.
systemctl stop codeaf-org 2>/dev/null || true
rm -f /usr/local/bin/codeaf
install -m 755 "$here/codeaf" /usr/local/bin/codeaf
install -m 644 "$here/codeaf-org.service" /etc/systemd/system/codeaf-org.service
sed "s/{HOST}/$host/" "$here/Caddyfile" > /etc/caddy/Caddyfile
systemctl daemon-reload
systemctl enable --now codeaf-org
systemctl restart codeaf-org
systemctl reload caddy || systemctl restart caddy
