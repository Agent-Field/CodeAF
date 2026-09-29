#!/usr/bin/env bash
# The campaign's model key, out of band.
#
#   bench/frontiercode/gcp-key.sh --install <instance>   push the key, print only its length and a digest prefix
#   bench/frontiercode/gcp-key.sh --usage   [instance]   read the provider's usage counter (local secret store unless an instance is named)
#   bench/frontiercode/gcp-key.sh --shred   <instance>   destroy the host's copy before teardown
#
# The key lives in the local secret store the manifest names — one key per
# campaign, so the provider's meter isolates that campaign's spend. NO PATH IN
# THIS SCRIPT EVER PRINTS THE VALUE: --install prints its length, a SHA-256
# prefix and the provider's usage counter; --shred prints only that the copy is
# gone. The counter this prints is the number the preregistration records.
set -euo pipefail
FC_SCRIPT=gcp-key.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gcp-lib.sh"

MODE="${1:-}"; INSTANCE="${2:-}"
case "$MODE" in --install|--shred|--usage) ;; *) sed -n '2,14p' "$0" >&2; exit 2 ;; esac
case "$MODE" in --install|--shred) [ -n "$INSTANCE" ] || fc_die "instance is required" ;; esac

fc_load_manifest
fc_require_keys .key.store .key.item .gcp.zone

PROJECT="${GCP_PROJECT:-$(fc_get '.gcp.project')}"
ZONE="$(fc_get '.gcp.zone')"

# The one place a key value is read. Every branch below pipes it into a
# transfer or a metered API call; none echoes it.
read_key() { fc_secret_read; }

usage_from_stdin() {
  # The counter is read with the key on stdin, so it never appears in argv.
  curl -sS -m 20 https://openrouter.ai/api/v1/key \
    -H "Authorization: Bearer $(read_key)" \
    | jq -c '{label: .data.label, usage_usd: .data.usage, limit: .data.limit}'
}

if [ "$MODE" = --usage ]; then
  if [ -n "$INSTANCE" ]; then
    gcloud compute ssh "$INSTANCE" --project="$PROJECT" --zone="$ZONE" --quiet --command='
      . ~/.codeaf-key 2>/dev/null || { echo "no key on this host" >&2; exit 3; }
      curl -sS -m 20 https://openrouter.ai/api/v1/key -H "Authorization: Bearer $OPENROUTER_API_KEY" \
        | jq -c "{usage_usd: .data.usage, limit: .data.limit}"' 2>&1 | grep -v '^Warning: Permanently added'
  else
    usage_from_stdin
  fi
  exit 0
fi

if [ "$MODE" = --shred ]; then
  gcloud compute ssh "$INSTANCE" --project="$PROJECT" --zone="$ZONE" --quiet --command='
    if [ -f ~/.codeaf-key ]; then shred -u ~/.codeaf-key; echo "host key shredded"; else echo "no host key file present"; fi' \
    2>&1 | grep -v '^Warning: Permanently added'
  exit 0
fi

# --install. The remote script prints the length, a digest prefix and the
# provider counter; the transfer itself is the only place the value is used.
printf 'export OPENROUTER_API_KEY=%s\n' "$(read_key)" \
  | gcloud compute ssh "$INSTANCE" --project="$PROJECT" --zone="$ZONE" --quiet --command='
      umask 077
      cat > ~/.codeaf-key
      chmod 600 ~/.codeaf-key
      . ~/.codeaf-key
      printf "key on %s: length %s sha256 %s\n" "$(hostname)" "${#OPENROUTER_API_KEY}" "$(printf %s "$OPENROUTER_API_KEY" | sha256sum | cut -c1-8)"
      curl -sS -m 20 https://openrouter.ai/api/v1/key -H "Authorization: Bearer $OPENROUTER_API_KEY" \
        | jq -c "{usage_usd: .data.usage, limit: .data.limit}"' \
    2>&1 | grep -v '^Warning: Permanently added'
