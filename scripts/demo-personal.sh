#!/usr/bin/env bash
# demo-personal.sh — a FIXTURE profile for looking through the folders place:
# Startup, with Product and Marketing inside it; one chat filed in both; a
# finished piece of work; a file; a folder rule; ongoing work over local files
# placed in Product with its report; a chat filed nowhere; and one filed record
# whose conversation is not on the machine.
#
# Every folder, filing, placement, rule and watch is made through the shipped
# binary's own doors (aforge collections …, aforge standing add …). The only
# records written around them are the ones no door makes because they are
# history — three conversations and one finished task — and those come from
# aforge-demo-home --personal, where every word says "(fixture)".
#
# NO MODEL IS CALLED, nothing is sent anywhere, no timer is installed, and HOME
# is never changed: the profile is AFORGE_HOME and nothing else. The one
# `aforge standing check` below takes the watch's baseline, which runs nothing.
#
# Usage:
#   make build
#   scripts/demo-personal.sh /path/to/new-empty-profile
#   AFORGE_HOME=/path/to/new-empty-profile bin/aforge     # then press alt+8
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/bin/aforge"
seeder="$root/bin/aforge-demo-home"
profile="${1:-${DEMO_PROFILE:-}}"
if [[ -z "$profile" ]]; then
  echo "usage: scripts/demo-personal.sh <new-empty-profile-directory>" >&2
  exit 2
fi
if [[ ! -x "$bin" ]]; then
  echo "bin/aforge is missing: run make build first." >&2
  exit 2
fi
if [[ -e "$profile" && -n "$(ls -A "$profile" 2>/dev/null)" ]]; then
  echo "$profile is not empty; the fixture only writes a new profile." >&2
  exit 2
fi
# The seeder is the demo home's own developer binary, built exactly as
# `make demo-home` builds it, so bin/aforge does not change by a byte.
go build -o "$seeder" "$root/cmd/aforge-demo-home"

manifest="$("$seeder" --personal --into "$profile")"
field() { python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$manifest" "$1"; }
profile="$(field state)"
project="$(field project)"
shared="$(field shared)" roadmap="$(field roadmap)" unfiled="$(field unfiled)"
task="$(field taskId)" spec="$(field spec)" report="$(field report)"

export AFORGE_HOME="$profile"
unset AFORGE_PROFILE_DIR || true
# Memory off and background checks off: this profile is for looking, and the
# machine's timer belongs to the person's real install.
printf '{"memory.enabled":"off","standing.background":"off"}\n' > "$AFORGE_HOME/config.json"

say() { printf '\n== %s\n' "$*"; }
run() { printf '$ aforge %s\n' "$*" >&2; "$bin" "$@"; }
idof() { sed -E 's/.*"id":"([^"]+)".*/\1/'; }

say "Folders"
startup="$("$bin" collections create Startup --json | idof)"
product="$("$bin" collections create Product --json | idof)"
marketing="$("$bin" collections create Marketing --json | idof)"
run collections add "$startup" collection "$product" >/dev/null
run collections add "$startup" collection "$marketing" >/dev/null

say "Filed: one chat in both folders, work, files, and a record that is gone"
run collections add "$product" conversation "$shared" >/dev/null
run collections add "$marketing" conversation "$shared" >/dev/null
run collections add "$product" conversation "$roadmap" >/dev/null
run collections add --session "$roadmap" "$product" task "$task" >/dev/null
run collections add "$product" artifact "$spec" >/dev/null
run collections add "$product" artifact "$report" >/dev/null
run collections add "$marketing" artifact "$project/marketing/launch-copy.md" >/dev/null
# A conversation id no bucket holds: the place must say `missing`, not hide it.
missing="ffffffffffff0009"
run collections add "$product" conversation "$missing" >/dev/null

say "A rule for Product, and ongoing work placed in Product"
run standing add --hold --scope "$product" --workspace "$project" \
  --words "(fixture) Product reports never quote customer contact details." >/dev/null
digest="$("$bin" standing add --json --workspace "$project" --place "$product" \
  --words "(fixture) Keep the product digest current from the spec." \
  --watch 'product/*' --report reports/product-digest.md --per-run-usd 0.05 \
  --instructions "Read the files under product/ that changed and rewrite reports/product-digest.md as a short Markdown digest. Do not change any other file." \
  | head -1 | idof)"
[[ -n "$digest" ]] || { echo "the ongoing work was not made" >&2; exit 1; }

say "The watch's baseline (runs nothing, calls no model)"
run standing check

say "Receipts"
run collections find conversation "$shared" --json
run collections find standing "$digest" --json
cat > "$profile/fixture/manifest.json" <<EOF
{"profile":"$profile","project":"$project","startup":"$startup","product":"$product","marketing":"$marketing",
 "sharedChat":"$shared","roadmapChat":"$roadmap","unfiledChat":"$unfiled","task":"$task","taskSession":"$roadmap",
 "ongoingWork":"$digest","missingChat":"$missing","spec":"$spec","report":"$report"}
EOF
say "Ready"
echo "manifest: $profile/fixture/manifest.json"
echo "open it:  cd $project && AFORGE_HOME=$profile $bin    # alt+8 is folders"
