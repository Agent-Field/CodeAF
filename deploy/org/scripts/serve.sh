#!/bin/sh
# Writes the organization server's config from the environment, then runs it.
# Nothing secret is baked into the image: the access token is generated on the
# first start (or taken from CODEAF_FACTORY_TOKEN) and lives in the data volume.
set -eu

: "${CODEAF_HOME:=/data/codeaf}"
dir="$CODEAF_HOME/factory"
config="$dir/config.yaml"
umask 077
mkdir -p "$dir"

port="${PORT:-8787}"
listen="${CODEAF_ORG_LISTEN:-0.0.0.0:$port}"
name="${CODEAF_ORG_NAME:-codeaf organization}"
domain="${CODEAF_PUBLIC_DOMAIN:-}"

# Keep the token the server minted on an earlier start, so a restart signs nobody out.
token="${CODEAF_FACTORY_TOKEN:-}"
if [ -z "$token" ] && [ -f "$config" ]; then
  token=$(sed -n 's/^access_token: *"\{0,1\}\([0-9a-f]*\)"\{0,1\}$/\1/p' "$config" | head -n 1)
fi

{
  echo "mode: team"
  echo "name: \"$name\""
  echo "listen: $listen"
  # TLS is the platform's job here (Railway, Caddy, a load balancer); this process speaks plain HTTP.
  echo "tls: \"off\""
  echo "access_token: \"$token\""
  [ -n "$domain" ] && echo "domain: $domain"
  [ -n "${CODEAF_GITHUB_CLIENT_ID:-}" ] && echo "github_client_id: ${CODEAF_GITHUB_CLIENT_ID}"
  if [ -n "${CODEAF_TRUSTED_PROXIES:-}" ]; then
    echo "trusted_proxies:"
    echo "$CODEAF_TRUSTED_PROXIES" | tr ',' '\n' | while read -r p; do
      p=$(echo "$p" | tr -d ' ')
      [ -n "$p" ] && echo "  - \"$p\""
    done
  fi
} > "$config.new"
mv "$config.new" "$config"

exec codeaf factory serve --config "$config" "$@"
