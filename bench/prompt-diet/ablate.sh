#!/usr/bin/env bash
# ablate.sh — remove ONE law unit from the page and run the cells without it.
#
# PREPARED, NOT YET RUN. Nothing in this wave's evidence comes from this file
# yet; it exists so that the arbiter DESIGN.md §3 calls for ("ablation as the
# arbiter") is a script somebody can start rather than a paragraph somebody has
# to re-derive. Read docs/design/prompt-diet/BENCH.md §5 before spending on it.
#
# WHY IT EXISTS. Exactly one paragraph on the page has ever earned its place
# with evidence: the working discipline, on twelve unattended runs, 3/3 against
# 0/5. Every other law is there because somebody believed it helped. An ablation
# is the only instrument that can tell the two apart — render the page with one
# unit missing, run the same cells, and see whether the outcome moves. What does
# not move is not deleted; it is refiled as ON DEMAND, which is the diet's whole
# thesis.
#
# IT DOES NOT DECIDE ANYTHING ON ITS OWN. A cell set this size cannot separate a
# real effect from the model's own variance in one run, and a pinned law does
# not move on one run's evidence whatever it says (the brief's list, and
# DESIGN.md §6). Treat a moved outcome as a reason to run that unit again with
# more seeds, never as a verdict.
#
# TWO ROADS TO REMOVING A UNIT, and it takes whichever exists:
#
#   the env    AFORGE_PROMPT_ABLATE=<id> — a hook lane C or lane G may add to
#              the law registry, which drops one registered unit from the
#              rendered page and leaves everything else byte-identical. This is
#              the honest road: the unit is removed by the same code that puts
#              it there, so no neighbouring byte moves.
#   the patch  failing that, the unit's key sentence is cut out of a THROWAWAY
#              worktree's `internal/session/prompts/system.md` with python, and
#              the binary is rebuilt from it. Cruder — a cut can take a heading
#              or a blank line with it — but it needs nothing from another lane.
#
# The script asks the binary which road it is on and says so in the row. A run
# that silently fell back to patching when the caller expected the env hook
# would be comparing two different experiments under one name.
#
# Usage:
#   bench/prompt-diet/ablate.sh <branch> <unit-id> [options]
#   bench/prompt-diet/ablate.sh prompt-diet/integrate media-essay --dry-run
#
# Options are run.sh's, and are passed through unchanged.
set -uo pipefail

DIET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RIG_ROOT="$(cd "$DIET_ROOT/../.." && pwd)"
UNITS="$DIET_ROOT/units.tsv"

BRANCH="${1:-}"
UNIT="${2:-}"
if [ -z "$BRANCH" ] || [ -z "$UNIT" ]; then
  sed -n '1,45p' "$0"
  echo
  echo "units (from $UNITS):"
  awk -F'\t' '!/^#/ && NF {printf "  %-22s %s\n", $1, $3}' "$UNITS" 2>/dev/null
  exit 1
fi
shift 2

row="$(awk -F'\t' -v want="$UNIT" '!/^#/ && $1 == want' "$UNITS" 2>/dev/null | head -1)"
if [ -z "$row" ]; then
  echo "!  no unit named $UNIT in $UNITS" >&2
  exit 1
fi
KEY_SENTENCE="$(cut -f2 <<< "$row")"
WHERE="$(cut -f4 <<< "$row")"
PINNED="$(cut -f6 <<< "$row")"

LABEL="ablate-$UNIT"
# The same two roots run.sh uses, and for the same reason: a cell workspace
# nested inside a checkout hands the model the harness instead of the fixture.
OUT="${DIET_OUT_ROOT:-$HOME/bench-diet-out}/$LABEL"
WORKTREE="${DIET_BUILD_ROOT:-$HOME/bench-diet-build}/$LABEL"
mkdir -p "$OUT" "$(dirname "$WORKTREE")"

# Whether the env hook exists is a question about the tree, not about the
# binary: the registry is a Go symbol and grepping for it is both cheaper and
# more honest than launching something and reading its behaviour.
ROAD="patch"
if grep -rqs 'AFORGE_PROMPT_ABLATE' "$RIG_ROOT/internal/session/" 2>/dev/null; then
  ROAD="env"
fi
echo "unit:  $UNIT"
echo "road:  $ROAD"
echo "where: $WHERE"
echo "key:   $KEY_SENTENCE"
echo "out:   $OUT"
[ "$PINNED" = "yes" ] && echo "note:  this law is PINNED — the result is a note, never a deletion"

# A unit rendered from Go cannot be cut out of a Markdown file, and a script
# that tried would remove nothing and report a null effect — which reads exactly
# like a law that turned out not to matter. Refusing is the only honest answer
# until the hook exists.
if [ "$ROAD" = "patch" ] && [ "$WHERE" != "system.md" ]; then
  echo "!  $UNIT is rendered from $WHERE, and the patch road can only cut system.md." >&2
  echo "!  It needs the AFORGE_PROMPT_ABLATE hook (lane C or lane G). Not run." >&2
  exit 1
fi

if [ "$ROAD" = "env" ]; then
  AFORGE_PROMPT_ABLATE="$UNIT" \
    "$DIET_ROOT/run.sh" "$BRANCH" "$LABEL" --out "$OUT" --layers a,c,d "$@"
  exit $?
fi

# ── the patch road ──────────────────────────────────────────────────────────
#
# The worktree is built and then EDITED, and run.sh is pointed at it with
# --reuse-worktree so it rebuilds rather than resetting the tree. The edit is
# never made anywhere but here: `git worktree remove --force` at the end takes
# the whole thing, and nothing in the caller's checkout is touched.
rm -rf "$WORKTREE"
git -C "$RIG_ROOT" fetch -q origin "$BRANCH" 2>/dev/null || true
RESOLVED=""
for candidate in "origin/$BRANCH" "$BRANCH" FETCH_HEAD; do
  git -C "$RIG_ROOT" rev-parse --verify -q "$candidate^{commit}" >/dev/null 2>&1 \
    && { RESOLVED="$candidate"; break; }
done
[ -n "$RESOLVED" ] || { echo "!  cannot resolve $BRANCH" >&2; exit 1; }
git -C "$RIG_ROOT" worktree add -f --detach "$WORKTREE" "$RESOLVED" >/dev/null 2>&1 \
  || { echo "!  git worktree add failed" >&2; exit 1; }

PAGE="$WORKTREE/internal/session/prompts/system.md"
KEY="$KEY_SENTENCE" python3 - "$PAGE" "$OUT/ablation.json" <<'PY'
import json, os, sys

page, receipt = sys.argv[1], sys.argv[2]
key = os.environ["KEY"]
text = open(page, encoding="utf-8").read()

# THE UNIT IS A PARAGRAPH, NOT A LINE. A law that lost its first sentence and
# kept its second is not an ablation of that law — it is a new law nobody wrote,
# and the run would be measuring it instead. So the whole blank-line-delimited
# block the key sentence sits in goes, and the receipt records exactly what was
# removed so a surprising result can be read against the actual bytes.
blocks = text.split("\n\n")
hit = [index for index, block in enumerate(blocks) if key in block]
if not hit:
    sys.stderr.write("the key sentence is not on this page: %r\n" % key)
    sys.exit(1)
removed = [blocks[index] for index in hit]
kept = [block for index, block in enumerate(blocks) if index not in hit]
open(page, "w", encoding="utf-8").write("\n\n".join(kept))
json.dump({"key": key, "blocks_removed": len(removed), "removed": removed,
           "bytes_before": len(text), "bytes_after": len("\n\n".join(kept))},
          open(receipt, "w"), indent=2)
print("removed %d block(s), %d bytes" % (len(removed), len(text) - len("\n\n".join(kept))))
PY
[ $? -eq 0 ] || { git -C "$RIG_ROOT" worktree remove --force "$WORKTREE"; exit 1; }

DIET_WORKTREE="$WORKTREE" \
  "$DIET_ROOT/run.sh" "$BRANCH" "$LABEL" --out "$OUT" --layers a,c,d \
    --reuse-worktree "$@"
code=$?

# The budget test is expected to FAIL nothing here — the page got smaller — but
# `prompt_belt_test` and the manual gates may well complain, and that is
# information: a unit whose removal breaks a law test is a unit that is pinned,
# and pinned laws do not move on an ablation's evidence.
echo "ablation receipt: $OUT/ablation.json"
exit $code
