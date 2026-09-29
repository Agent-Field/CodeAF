#!/usr/bin/env bash
# The campaign's model key, out of band.
#
#   bench/frontiercode/gcp-key.sh --plan   [instance]   print what would happen, transfer nothing
#   bench/frontiercode/gcp-key.sh --check  [instance]   verify the store holds the item, transfer nothing
#   bench/frontiercode/gcp-key.sh --install <instance>   push the key, print only its length and a digest prefix
#   bench/frontiercode/gcp-key.sh --usage   [instance]   read the provider's usage counter (local secret store unless an instance is named)
#   bench/frontiercode/gcp-key.sh --shred   <instance>   destroy the host's copy before teardown
#
# The key lives in the local secret store the manifest names and may be reused
# across campaigns. A campaign's provider-side spend is the DELTA of the usage
# counter --install prints (the baseline) and the one --usage prints at
# teardown, which is this campaign's share only while nothing else uses the key
# in that window. NO PATH IN THIS SCRIPT EVER PRINTS THE VALUE: --install prints
# its length, a SHA-256 prefix and the usage counter; --shred prints only that
# the copy is gone.
set -euo pipefail
FC_SCRIPT=gcp-key.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gcp-lib.sh"

MODE="${1:-}"; INSTANCE="${2:-}"
case "$MODE" in --plan|--check|--install|--shred|--usage) ;; *) sed -n '2,16p' "$0" >&2; exit 2 ;; esac
case "$MODE" in --install|--shred) [ -n "$INSTANCE" ] || fc_die "instance is required" ;; esac

fc_load_manifest
fc_require_keys .key.store .key.item .gcp.zone

PROJECT="${GCP_PROJECT:-$(fc_get '.gcp.project')}"
ZONE="$(fc_get '.gcp.zone')"

if [ "$MODE" = --plan ]; then
  cat <<PLAN
secret store:    $(fc_get '.key.store') item '$(fc_get '.key.item')' (the value is never read by --plan)
target:          ${INSTANCE:-<none named>} ($PROJECT / $ZONE)
--install:       push the key over ssh, print only length, sha256 prefix and the provider usage counter
--shred:         destroy the host's copy before teardown
--usage:         read the provider's usage counter, locally or on the host
PLAN
  echo "PLAN ONLY: no key read, no host contacted, nothing transferred"
  exit 0
fi

if [ "$MODE" = --check ]; then
  # Prove the store actually holds the item without ever printing it: read it
  # into a variable, report its length only, and drop it.
  k="$(fc_secret_read)"
  [ -n "$k" ] || fc_die "the secret store does not hold '$(fc_get '.key.item')'"
  echo "secret store holds '$(fc_get '.key.item')' (length ${#k}; value not printed)"
  unset k
  if [ -n "$INSTANCE" ]; then
    gcloud compute instances describe "$INSTANCE" --project="$PROJECT" --zone="$ZONE" \
      --format='value(status)' 2>/dev/null | grep -qx RUNNING \
      || fc_die "instance is not RUNNING: $INSTANCE"
    echo "instance $INSTANCE is RUNNING"
  fi
  echo "CHECK ONLY: nothing transferred"
  exit 0
fi

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
