#!/usr/bin/env bash
# gate.sh — Part 11.1's parity gate, measured.
#
# 11.1 says the default flips to v2 only when the parity checklist passes, and
# names this suite's journeys as part of that checklist. 13.4 found the gate
# unmeasurable: every journey launched v1 and nothing under test/ set --v2. So
# this runs the same journeys twice — once against `aforge chat`, once against
# `aforge chat --v2` — and lays the two verdicts side by side.
#
#   test/ux/gate.sh                    both surfaces, journeys + quality
#   test/ux/gate.sh --suite journeys   the 19 journeys only
#   test/ux/gate.sh --only 06,10,13    a subset, both surfaces
#   test/ux/gate.sh --compare-only     re-read the two evidence trees and
#                                      rewrite parity.md, spending nothing —
#                                      also how to build the table when the two
#                                      surfaces were run in parallel by hand
#                                      (`run.sh --surface v1 &` `--surface v2 &`)
#
# Every flag is passed straight through to run.sh, except --surface, which is
# what this script is for. Each surface gets its OWN disposable brain: they
# never share a journal, because journey 5 corrects the haiku journey 2 wrote
# and a shared store would let one surface pass on the other's work.
#
# The binary is built ONCE and both runs are told to use it (UX_SKIP_BUILD), so
# a tree that moves under a long run cannot make the two halves of a
# comparison different programs.
#
# Exit status is the gate itself: non-zero when a journey that v1 passes is not
# passed by v2. A journey both surfaces fail is a product gap, not a parity
# regression, and it is listed but does not fail the gate — the gate asks
# whether the NEW surface has caught up, and the report says the rest.

set -uo pipefail

UX_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$UX_ROOT/../.." && pwd)"

COMPARE_ONLY=0
ARGS=()
for arg in "$@"; do
  case "$arg" in
    --compare-only) COMPARE_ONLY=1 ;;
    *) ARGS+=("$arg") ;;
  esac
done
set -- ${ARGS+"${ARGS[@]}"}

if [ "$COMPARE_ONLY" = "1" ]; then
  v1_code=compare-only
  v2_code=compare-only
else

BIN="${UX_BIN:-$REPO/bin/aforge}"
# UX_SKIP_BUILD=1 already in the environment means somebody has handed us the
# binary to measure — a build from a known commit while the working tree is
# mid-edit, say. Otherwise build it here, once, for both surfaces.
if [ "${UX_SKIP_BUILD:-0}" = "1" ]; then
  [ -x "$BIN" ] || { echo "UX_SKIP_BUILD=1 but $BIN is not executable" >&2; exit 2; }
  echo "measuring $BIN (build skipped)"
else
  echo "building $BIN once, for both surfaces"
  ( cd "$REPO" && go build -o "$BIN" ./cmd/aforge ) || exit 2
fi
export UX_BIN="$BIN" UX_SKIP_BUILD=1

echo
echo "════ surface v1 — aforge chat ════"
"$UX_ROOT/run.sh" --surface v1 "$@"
v1_code=$?

echo
echo "════ surface v2 — aforge chat --v2 ════"
"$UX_ROOT/run.sh" --surface v2 "$@"
v2_code=$?

fi

python3 - "$UX_ROOT" "$v1_code" "$v2_code" <<'PY'
import csv, os, sys

root, v1_code, v2_code = sys.argv[1], sys.argv[2], sys.argv[3]


def gauges(surface):
    path = os.path.join(root, "evidence-%s" % surface, "gauges.csv")
    rows = {}
    try:
        with open(path) as handle:
            for row in csv.DictReader(handle):
                rows[row["journey"]] = row
    except FileNotFoundError:
        pass
    return rows


def checks(surface, journey):
    path = os.path.join(root, "evidence-%s" % surface, journey, "checks")
    try:
        return open(path).read().strip().replace(" checks", "")
    except OSError:
        return "—"


one, two = gauges("v1"), gauges("v2")
journeys = sorted(set(one) | set(two))

# PASS and OBSERVED both mean "nothing was found wrong here". SKIPPED means the
# journey declined to measure itself and cannot testify either way.
def ok(verdict):
    return verdict in ("PASS", "OBSERVED")


lines = []
lines.append("# The 11.1 parity gate — v1 beside v2")
lines.append("")
lines.append("The same journeys, the same sentences, two surfaces. Where v2 legitimately")
lines.append("renders something elsewhere (13.4), the v2 assertion follows the design doc and")
lines.append("says so at the assertion. Where v2 simply cannot do the thing yet, the journey")
lines.append("is red and stays red.")
lines.append("")
lines.append("| journey | v1 | v2 | v1 checks | v2 checks | v1 $ | v2 $ | gate |")
lines.append("|---|---|---|---|---|---|---|---|")

regressions, both_red, only_v2 = [], [], []
for journey in journeys:
    a, b = one.get(journey), two.get(journey)
    va = a["verdict"] if a else "not run"
    vb = b["verdict"] if b else "not run"
    if ok(va) and not ok(vb):
        mark, note = "❌ v2 behind", regressions
    elif not ok(va) and not ok(vb):
        mark, note = "· both red", both_red
    elif not ok(va) and ok(vb):
        mark, note = "＋ v2 ahead", only_v2
    else:
        mark, note = "✅ parity", None
    if note is not None:
        note.append(journey)
    lines.append("| %s | %s | %s | %s | %s | $%s | $%s | %s |" % (
        journey, va, vb, checks("v1", journey), checks("v2", journey),
        (a or {}).get("cost", "—"), (b or {}).get("cost", "—"), mark))

lines.append("")
if regressions:
    lines.append("**GATE OPEN — %d journey(s) v1 passes and v2 does not**: %s"
                 % (len(regressions), ", ".join(regressions)))
else:
    lines.append("**GATE CLOSED on the journeys measured here** — v2 passes everything v1 passes.")
if both_red:
    lines.append("")
    lines.append("Failing on both surfaces (a product gap, not a parity gap): %s"
                 % ", ".join(both_red))
if only_v2:
    lines.append("")
    lines.append("Passing on v2 and not on v1: %s" % ", ".join(only_v2))
lines.append("")
lines.append("Run exit codes — v1: %s · v2: %s. Reports: `report-v1.md`, `report-v2.md`."
             % (v1_code, v2_code))
lines.append("")

section = "\n".join(lines)
with open(os.path.join(root, "parity.md"), "w") as handle:
    handle.write(section)
print()
print(section)
sys.exit(1 if regressions else 0)
PY
