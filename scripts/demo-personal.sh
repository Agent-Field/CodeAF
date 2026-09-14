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
# `aforge standing check` below checks the watch against the listing it took
# when it was made, finds nothing changed, and runs nothing.
#
# STAGE=2 is checkpoint 2's fixture: the same folders, chats, task and rule, but
# NO ongoing work and no report file — a fourth fixture conversation, Product
# reports, is filed and PLACED in Product (so its rules reach work set up there)
# and the person sets the work up through that chat. The spec carries one fixture contact detail for the rule.
#
# Usage:
#   make build
#   scripts/demo-personal.sh /path/to/new-empty-profile
#   STAGE=2 scripts/demo-personal.sh /path/to/new-empty-profile
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
stage="${STAGE:-1}"
if [[ "$stage" != 1 && "$stage" != 2 ]]; then
  echo "STAGE is 1 or 2, not $stage" >&2
  exit 2
fi
# The seeder is the demo home's own developer binary, built exactly as
# `make demo-home` builds it, so bin/aforge does not change by a byte.
go build -o "$seeder" "$root/cmd/aforge-demo-home"
manifest="$("$seeder" --personal --stage "$stage" --into "$profile")"
field() { python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$manifest" "$1"; }
profile="$(field state)"
project="$(field project)"
shared="$(field shared)" roadmap="$(field roadmap)" unfiled="$(field unfiled)"
task="$(field taskId)" spec="$(field spec)" report="$(field report)"
reports="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("reports",""))' "$manifest")"

export AFORGE_HOME="$profile"
unset AFORGE_PROFILE_DIR || true
# Memory off and background checks off: this profile is for looking, and the
# machine's timer belongs to the person's real install.
#
# THE MODELS THE CHAT, ITS ROLES AND ONGOING WORK RUN ON ARE OPEN-WEIGHT, AND
# THEY ARE WRITTEN DOWN RATHER THAN INHERITED (the owner's ruling, 2026-09-14).
# The talk row is what a conversation opens on and what an ongoing-work run
# fires under (cmd/aforge's v3TalkModel), so work set up from the chat runs on it
# too. The fallback row is written because an empty one lets the catalog pick the
# "nearest" model on a refusal, which nobody chose and nothing keeps open; the
# five tiers and the looking row are written so no default can move under the
# fixture. Weights and licences were checked on the day: GLM-5.3-Flash and both
# DeepSeek Flash models MIT, Qwen3.8-27B and Mistral-Nemo Apache-2.0.
#
# WHAT THIS DOES NOT PIN, said rather than implied: the making verbs (image,
# speech, music, video) and listening or watching resolve from the catalog and
# its curated names, several of them closed, and the chat may name a model for
# one. Nothing in this fixture asks for them; the call log is the receipt. The
# model variables are unset here because AFORGE_VISION_MODEL and the
# AFORGE_*_MODEL making slots beat the rows; the launch line below unsets the two
# that name a row this profile writes (the talk row already beats AFORGE_MODEL).
unset AFORGE_MODEL AFORGE_PLAN_MODEL AFORGE_MODELS AFORGE_VISION_MODEL AFORGE_IMAGE_MODEL \
  AFORGE_SPEECH_MODEL AFORGE_MUSIC_MODEL AFORGE_VIDEO_MODEL AFORGE_VOICE_MODEL || true
talk="${DEMO_MODEL:-z-ai/glm-5.3-flash}"
# The failing model is never its own fallback, so a talk row on the fallback's
# model falls back to the default talk model instead of to the catalog's guess.
fallback="deepseek/deepseek-v4.1-flash"
[[ "$talk" != "$fallback" ]] || fallback="z-ai/glm-5.3-flash"
printf '{"memory.enabled":"off","standing.background":"off","model.talk":"%s","models.fallbacks":"%s","models.tiers.reflex":"mistralai/mistral-nemo","models.tiers.low":"deepseek/deepseek-v4-flash-0731","models.tiers.worker":"%s","models.tiers.high":"qwen/qwen3.8-27b","models.tiers.mastermind":"%s","vision_model":"qwen/qwen3.8-27b"}\n' \
  "$talk" "$fallback" "$talk" "$talk" > "$AFORGE_HOME/config.json"

say() { printf '\n== %s\n' "$*"; }
run() { printf '$ aforge %s\n' "$*" >&2; "$bin" "$@"; }
idof() { python3 -c 'import json,sys; print(json.loads(sys.stdin.readline())["id"])'; }

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
if [[ "$stage" == 1 ]]; then
  run collections add "$product" artifact "$report" >/dev/null
fi
run collections add "$marketing" artifact "$project/marketing/launch-copy.md" >/dev/null
# A conversation id no bucket holds: the place must say `missing`, not hide it.
missing="ffffffffffff0009"
run collections add "$product" conversation "$missing" >/dev/null

say "A rule for Product"
run standing add --hold --scope "$product" --workspace "$project" \
  --words "(fixture) Product reports never quote customer contact details." >/dev/null
digest=""
if [[ "$stage" == 1 ]]; then
  say "Ongoing work placed in Product"
  digest="$("$bin" standing add --json --workspace "$project" --place "$product" \
    --words "(fixture) Keep the product digest current from the spec." \
    --watch 'product/*' --report reports/product-digest.md --per-run-usd 0.05 \
    --instructions "Read the files under product/ that changed and rewrite reports/product-digest.md as a short Markdown digest. Do not change any other file." \
    | head -1 | idof)"
  [[ -n "$digest" ]] || { echo "the ongoing work was not made" >&2; exit 1; }
  say "One check: nothing has changed, nothing runs, no model is called"
  run standing check
else
  say "Product reports is filed and PLACED in Product: work set up in that chat inherits Product's rules"
  run collections add "$product" conversation "$reports" >/dev/null
  run collections place "$product" conversation "$reports" >/dev/null
fi

say "Receipts"
run collections find conversation "$shared" --json
[[ -z "$reports" ]] || run collections find conversation "$reports" --json
[[ -z "$digest" ]] || run collections find standing "$digest" --json
cat > "$profile/fixture/manifest.json" <<EOF
{"stage":$stage,"profile":"$profile","project":"$project","startup":"$startup","product":"$product","marketing":"$marketing",
 "sharedChat":"$shared","roadmapChat":"$roadmap","unfiledChat":"$unfiled","task":"$task","taskSession":"$roadmap",
 "reportsChat":"$reports","ongoingWork":"$digest","missingChat":"$missing","spec":"$spec","report":"$report"}
EOF
say "Ready"
echo "manifest: $profile/fixture/manifest.json"
echo "open it:  cd $project && env -u AFORGE_MODEL -u AFORGE_VISION_MODEL AFORGE_HOME=$profile $bin    # alt+8 is folders"
