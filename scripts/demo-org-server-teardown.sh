#!/usr/bin/env bash
# demo-org-server-teardown.sh [-y] — delete everything demo-org-server-deploy.sh
# made for the codeaf teams demo, so it stops billing: every VM, disk and static
# address labelled purpose=codeaf-teams-demo, and the firewall rules named
# codeaf-teams-demo-* (firewall rules cannot carry labels). The server's state
# lives on the VM's disk and goes with it. Prints each thing it deletes.
#
#   GCP_PROJECT  default openaf-505800
set -euo pipefail
project=${GCP_PROJECT:-openaf-505800}
filter='labels.purpose=codeaf-teams-demo'
g() { gcloud --project "$project" --quiet "$@"; }

vms=$(g compute instances list --filter "$filter" --format='value(name,zone.basename())')
ips=$(g compute addresses list --filter "$filter" --format='value(name,region.basename())')
fws=$(g compute firewall-rules list --filter 'name~^codeaf-teams-demo-' --format='value(name)')

echo "in project $project this deletes:"
[ -n "$vms" ] && echo "$vms" | awk '{print "  vm and its disks  "$1"  ("$2")"}'
[ -n "$ips" ] && echo "$ips" | awk '{print "  static address    "$1"  ("$2")"}'
[ -n "$fws" ] && echo "$fws" | awk '{print "  firewall rule     "$1}'
if [ -z "$vms$ips$fws" ]; then echo "  nothing — already torn down"; fi
if [ "${1:-}" != "-y" ] && [ -n "$vms$ips$fws" ]; then
  read -r -p "delete all of it? [y/N] " yes; [ "$yes" = y ] || { echo "kept"; exit 1; }
fi

while read -r vm zone; do
  [ -n "$vm" ] || continue
  g compute instances delete "$vm" --zone "$zone" --delete-disks all && echo "deleted vm $vm and its disks"
done <<< "$vms"
# A disk that outlived its VM (auto-delete off, or a failed create) still bills.
while read -r disk zone; do
  [ -n "$disk" ] || continue
  g compute disks delete "$disk" --zone "$zone" && echo "deleted disk $disk"
done <<< "$(g compute disks list --filter "$filter" --format='value(name,zone.basename())')"
while read -r addr region; do
  [ -n "$addr" ] || continue
  g compute addresses delete "$addr" --region "$region" && echo "deleted static address $addr"
done <<< "$ips"
for fw in $fws; do
  g compute firewall-rules delete "$fw" && echo "deleted firewall rule $fw"
done
left=$(g compute instances list --filter "$filter" --format='value(name)'; g compute disks list --filter "$filter" --format='value(name)'; g compute addresses list --filter "$filter" --format='value(name)'; g compute firewall-rules list --filter 'name~^codeaf-teams-demo-' --format='value(name)')
if [ -n "$left" ]; then echo "still there:"; echo "$left"; exit 1; fi
echo "done: nothing of the codeaf teams demo is left in $project"
