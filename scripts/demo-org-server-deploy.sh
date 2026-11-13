#!/usr/bin/env bash
# demo-org-server-deploy.sh [ref] — build `codeaf` for linux/amd64 from a git
# ref (default origin/work/teams) and put it on the teams-demo organization
# server on GCP, creating the VM, static IP and firewall rules the first time.
# Run it again with a newer ref to update; state on the VM's disk is kept.
# /v1/about is the one route that answers without a session from off the box
# (/healthz is open only on loopback), so it is what proves the deploy.
#
# Everything it creates is labelled purpose=codeaf-teams-demo (firewall rules
# cannot carry labels, so they share the name prefix and the network tag), and
# scripts/demo-org-server-teardown.sh deletes all of it.
#
#   GCP_PROJECT  default openaf-505800
#   GCP_ZONE     default us-central1-a
set -euo pipefail

ref=${1:-origin/work/teams}
project=${GCP_PROJECT:-openaf-505800}
zone=${GCP_ZONE:-us-central1-a}
region=${zone%-*}
name=codeaf-teams-demo
label=purpose=codeaf-teams-demo
client_id=${CODEAF_GITHUB_CLIENT_ID:-Ov23liZE8n5kPnDn5RcK}
repo=$(git rev-parse --show-toplevel)
g() { gcloud --project "$project" --quiet "$@"; }

# ── the address, the door, the machine (each only when missing) ──────────────
if ! g compute addresses describe "$name-ip" --region "$region" >/dev/null 2>&1; then
  g compute addresses create "$name-ip" --region "$region"
fi
g compute addresses update "$name-ip" --region "$region" --update-labels "$label" >/dev/null
ip=$(g compute addresses describe "$name-ip" --region "$region" --format='value(address)')
host="${ip//./-}.sslip.io"

for rule in "web tcp:80,tcp:443" "ssh tcp:22"; do
  set -- $rule
  if ! g compute firewall-rules describe "$name-$1" >/dev/null 2>&1; then
    g compute firewall-rules create "$name-$1" --network default --direction INGRESS \
      --allow "$2" --target-tags "$name" --source-ranges 0.0.0.0/0 \
      --description "$label (codeaf teams demo; deleted by demo-org-server-teardown.sh)"
  fi
done

if ! g compute instances describe "$name" --zone "$zone" >/dev/null 2>&1; then
  g compute instances create "$name" --zone "$zone" --machine-type e2-small \
    --image-family debian-12 --image-project debian-cloud \
    --boot-disk-size 20GB --boot-disk-type pd-balanced \
    --address "$ip" --tags "$name" --labels "$label"
  g compute disks add-labels "$name" --zone "$zone" --labels "$label"
  echo "waiting for ssh on $name…"
  for _ in $(seq 1 30); do
    g compute ssh "$name" --zone "$zone" --command true >/dev/null 2>&1 && break
    sleep 5
  done
fi

# ── the build, from the ref, in a throwaway worktree ─────────────────────────
git -C "$repo" fetch --quiet origin
rev=$(git -C "$repo" rev-parse --short=9 "$ref")
work=$(mktemp -d); stage=$(mktemp -d)
trap 'git -C "$repo" worktree remove --force "$work/src" >/dev/null 2>&1 || true; rm -rf "$work" "$stage"' EXIT
git -C "$repo" worktree add --quiet --detach "$work/src" "$rev"
(
  cd "$work/src"
  make --quiet embed
  stamp="-X github.com/Agent-Field/codeaf/internal/buildinfo.rev=$rev -X github.com/Agent-Field/codeaf/internal/buildinfo.dirty=false"
  stamp+=" -X github.com/Agent-Field/codeaf/internal/buildinfo.builtAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  stamp+=" -X github.com/Agent-Field/codeaf/internal/factory/github.clientID=$client_id"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags=codeaf_packed_manual -trimpath \
    -ldflags="-s -w $stamp" -o "$stage/codeaf" ./cmd/codeaf
)
cp "$repo"/deploy/demo-org-server/{install.sh,codeaf-org.service,Caddyfile} "$stage/"

# ── install and restart ──────────────────────────────────────────────────────
g compute ssh "$name" --zone "$zone" --command 'rm -rf /tmp/codeaf-deploy && mkdir -p /tmp/codeaf-deploy'
g compute scp --zone "$zone" "$stage"/* "$name:/tmp/codeaf-deploy/"
g compute ssh "$name" --zone "$zone" --command "sudo bash /tmp/codeaf-deploy/install.sh '$host' '$client_id' && rm -rf /tmp/codeaf-deploy"

echo "checking https://$host/v1/about …"
for _ in $(seq 1 30); do
  if out=$(curl -fsS -m 5 "https://$host/v1/about" 2>/dev/null); then
    echo "$out"
    echo
    echo "organization address: https://$host   (built from $ref @ $rev)"
    echo "operator link (one-time, for the person who claims the server):"
    echo "  gcloud --project $project compute ssh $name --zone $zone --command \"sudo journalctl -u codeaf-org --no-pager | grep 'operator access link' | tail -1\""
    exit 0
  fi
  sleep 5
done
echo "https://$host/v1/about did not answer; see: gcloud --project $project compute ssh $name --zone $zone --command 'sudo journalctl -u codeaf-org -u caddy --no-pager | tail -50'" >&2
exit 1
