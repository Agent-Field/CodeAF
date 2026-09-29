#!/usr/bin/env bash
# Plan or create the campaign's GCP hosts, one per shard: <instance_prefix>-s1
# .. <instance_prefix>-sN. Nothing is created until --create is passed, and
# every host carries a max-run-duration with instance-termination-action=STOP,
# so a campaign left running cannot bill forever.
#
#   bench/frontiercode/gcp-create.sh --plan      print the complete plan, touch nothing
#   bench/frontiercode/gcp-create.sh --create    create the hosts, refusing an existing one
#
# --plan needs no gcloud credentials and creates nothing: the campaign plan is
# read from the manifest, and the instance listing is attempted only when
# gcloud is present and authenticated. --create never lists or touches an
# instance outside this campaign's own prefix.
#
# FC_MANIFEST selects the campaign manifest; GCP_PROJECT overrides the
# manifest's project for a one-off.
set -euo pipefail
FC_SCRIPT=gcp-create.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gcp-lib.sh"

MODE="${1:-}"
case "$MODE" in --plan|--create) ;; *) sed -n '2,12p' "$0" >&2; exit 2 ;; esac

fc_load_manifest
fc_require_keys .campaign_name .arm .seed_id .model .gcp.instance_prefix \
  .gcp.zone .gcp.image .gcp.machine_type .gcp.boot_disk_gb .gcp.max_run_duration

PROJECT="${GCP_PROJECT:-$(fc_get '.gcp.project')}"
ZONE="$(fc_get '.gcp.zone')"
IMAGE="$(fc_get '.gcp.image')"
MACHINE="$(fc_get '.gcp.machine_type')"
DISK_GB="$(fc_get '.gcp.boot_disk_gb')"
MAXRUN="$(fc_get '.gcp.max_run_duration')"
PREFIX="$(fc_get '.gcp.instance_prefix')"
ARM="$(fc_get '.arm')"
CAMPAIGN="$(fc_get '.campaign_name')"
SHARDS="$(fc_get '.host_shards')"
WAVE="$(fc_get '.wave_capacity')"
CPUS="$(fc_get '.per_container.cpus')"
MEMGB="$(fc_get '.per_container.memory_gb')"

case "$SHARDS" in ''|*[!0-9]*) fc_die "host_shards is not a number: $SHARDS" ;; esac
[ "$SHARDS" -ge 1 ] || fc_die "host_shards must be at least 1"

names=()
for ((s = 1; s <= SHARDS; s++)); do names+=("$PREFIX-s$s"); done

# The plan is the manifest rendered for a person: every number a host will be
# built with, and nothing else.
cat <<PLAN
campaign:        $CAMPAIGN
arm:             $ARM
seed:            $(fc_get '.seed_id')
model:           $(fc_get '.model')
list price:      input \$$(fc_get '.list_price_per_mtok.input')/Mtok  output \$$(fc_get '.list_price_per_mtok.output')/Mtok
codeaf commit:   $(fc_get '.codeaf_commit')  (bin/codeaf sha256 $(fc_get '.codeaf_sha256'))
rig commit:      $(fc_get '.rig_commit')
corpus:          $(fc_get '.corpus')  sha256 $(fc_get '.corpus_sha256')
host shards:     $SHARDS   hosts ${names[*]}
wave capacity:   $WAVE per host (at most ${WAVE} concurrent containers)
per container:   ${CPUS} CPU / ${MEMGB} GiB
gcp project:     $PROJECT
gcp zone/image:  $ZONE / $IMAGE
machine type:    $MACHINE
boot disk:       ${DISK_GB}GB pd-balanced
max run:         $MAXRUN, then STOP (instance-termination-action=STOP)
labels:          campaign=$(fc_slug "$CAMPAIGN"),lane=$(fc_slug "$ARM")
PLAN

# The listing is a convenience, never a requirement: --plan must work on a
# machine that has never run gcloud. Only this campaign's prefix is ever named.
if command -v gcloud >/dev/null 2>&1 && gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null | grep -q .; then
  echo "campaign hosts that already exist (prefix $PREFIX-s):"
  gcloud compute instances list --project="$PROJECT" \
    --filter="name ~ ^${PREFIX}-s" --format='table(name,zone,status)' 2>/dev/null || true
else
  echo "note: gcloud is absent or unauthenticated; skipping the instance listing (--plan needs neither)"
fi

if [ "$MODE" = --plan ]; then
  echo "PLAN ONLY: no instance created, nothing touched"
  exit 0
fi

command -v gcloud >/dev/null 2>&1 || fc_die "--create needs gcloud"
for n in "${names[@]}"; do
  if gcloud compute instances describe "$n" --project="$PROJECT" --zone="$ZONE" >/dev/null 2>&1; then
    fc_die "refusing to create: instance already exists: $n"
  fi
done

gcloud compute instances create "${names[@]}" \
  --project="$PROJECT" --zone="$ZONE" --machine-type="$MACHINE" \
  --image="$IMAGE" --boot-disk-size="${DISK_GB}GB" --boot-disk-type=pd-balanced \
  --provisioning-model=STANDARD \
  --labels="campaign=$(fc_slug "$CAMPAIGN"),lane=$(fc_slug "$ARM")" \
  --max-run-duration="$MAXRUN" --instance-termination-action=STOP --quiet

echo "created ${#names[@]} host(s): ${names[*]}"
echo "stage each one with ./gcp-stage.sh --stage <instance> <shard>; no credential has been copied"
