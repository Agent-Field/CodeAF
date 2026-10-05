#!/usr/bin/env bash
# One cell: one tool, one CVE, one variant. Called by run.sh (through xargs, so
# cells can run beside each other) with the run's settings in the environment.
#
#   cell.sh <cve> <variant>
#   needs: OUT TOOL MODEL MODE TIMEOUT RIG_SNAPSHOT (exported by run.sh)
#
# The tool gets a FRESH COPY of the checkout — tree/ or pr/ by MODE — and
# nothing else. info.json, which holds the answer, is copied into the cell only
# after the tool has exited. Whatever the tool left in the copy is discarded;
# the cell keeps what the driver wrote under $BENCH_OUT, the normalised
# findings.json, and meta.json.
set -uo pipefail
source "$RIG_SNAPSHOT/lib.sh"
CVE="$1"; VARIANT="$2"
CELL="$OUT/$CVE/$VARIANT"; mkdir -p "$CELL"
SRC="$WORK/repos/$CVE/$VARIANT"
M="$CELL/meta.json"
# A resumed run keeps every cell whose tool already finished; a cell the stop
# caught mid-tool has stage=tool and runs again from a fresh copy.
if [ "${RESUME:-0}" = 1 ] && [ -f "$M" ]; then
  case "$(jq -r '.stage // empty' "$M")" in
    judge|done|unavailable) log "$CVE/$VARIANT: kept from the earlier run"; exit 0 ;;
  esac
fi
meta "$M" "cve=$CVE" "variant=$VARIANT" "tool=$TOOL" "mode=$MODE" "model=$MODEL"

if [ -f "$WORK/repos/$CVE/unavailable.txt" ] || [ ! -s "$SRC/info.json" ]; then
  meta "$M" "stage=unavailable" "reason=$(cat "$WORK/repos/$CVE/unavailable.txt" 2>/dev/null | tr -d '\n' || echo 'not prepared')"
  log "$CVE/$VARIANT: unavailable"; exit 0
fi

case "$MODE" in
  diff) SHAPE="$SRC/pr" ;;
  repo) SHAPE="$SRC/tree" ;;
  *) meta "$M" "stage=bad-mode"; log "MODE must be diff or repo"; exit 2 ;;
esac

WS="$CELL/workspace"; rm -rf "$WS"; mkdir -p "$WS"
(cd "$SHAPE" && tar -c .) | tar -x -C "$WS"

export BENCH_CVE="$CVE" BENCH_VARIANT="$VARIANT" BENCH_MODE="$MODE" BENCH_WORKSPACE="$WS"
export BENCH_FILES="$SRC/pr-files.txt" BENCH_OUT="$CELL" BENCH_MODEL="$MODEL" BENCH_TIMEOUT="$TIMEOUT"
# The oracle driver is the one tool allowed to read the answer; it exists to
# prove the judge and the scorer can reach 1.0 on a correct report.
[ "$TOOL" = gold ] && export BENCH_INFO="$SRC/info.json"

DRIVER="$RIG_SNAPSHOT/tools/$TOOL.sh"
[ -x "$DRIVER" ] || chmod +x "$DRIVER"
rm -f "$CELL/findings.json"
t0=$(date +%s)
meta "$M" "stage=tool" "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
# The wall sits two minutes outside the budget the tool itself was handed, so a
# driver that honours BENCH_TIMEOUT ends on its own terms and only a hung one
# is killed from outside.
( cd "$WS" && with_wall "$((TIMEOUT + 120))" bash "$DRIVER" ) > "$CELL/tool.log" 2>&1
CODE=$?
SECS=$(( $(date +%s) - t0 ))

# Normalise what the driver wrote. A missing or malformed file is recorded as a
# tool error AND as an empty report, because that is what a reviewer received
# from the tool: nothing. score.py counts the errors separately so a crash can
# never pass for a quiet, correct review without being seen.
python3 - "$CELL/findings.json" "$M" "$CODE" "$SECS" <<'PY'
import json, os, sys
path, metapath, code, secs = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
meta = json.load(open(metapath))
err = None
findings = []
try:
    raw = json.load(open(path))
    items = raw["findings"] if isinstance(raw, dict) else raw
    if not isinstance(items, list):
        raise ValueError("findings is not a list")
    for i, f in enumerate(items):
        if not isinstance(f, dict):
            continue
        line = f.get("line")
        try:
            line = int(line) if line is not None else None
        except Exception:
            line = None
        findings.append({
            "file": str(f.get("file") or f.get("file_path") or f.get("path") or ""),
            "line": line,
            "title": str(f.get("title") or ""),
            "description": str(f.get("description") or f.get("explanation") or f.get("rationale") or ""),
            "cwe": f.get("cwe") or f.get("cwe_id"),
            "severity": f.get("severity"),
        })
except FileNotFoundError:
    err = "driver wrote no findings.json"
except Exception as e:
    err = "findings.json unreadable: %s" % e
if code != 0:
    err = (err + "; " if err else "") + "driver exited %d" % code
json.dump({"findings": findings}, open(path, "w"), indent=2)
meta.update({"exit_code": code, "tool_seconds": secs, "finding_count": len(findings),
             "tool_error": err, "stage": "judge"})
json.dump(meta, open(metapath, "w"), indent=2, sort_keys=True)
PY

cp "$SRC/info.json" "$CELL/truth.json"
[ "${KEEP_WORKSPACE:-0}" = 1 ] || rm -rf "$WS"
# The state root a driver ran under is debugging material, not evidence, and it
# is most of a cell's size. It is parked under bulk/ at the same relative path,
# so a results directory stays something a person can read whole and publish.
if [ -d "$CELL/home" ]; then
  park="$BULK/$(basename "$OUT")/$CVE/$VARIANT"
  rm -rf "$park"; mkdir -p "$park"; mv "$CELL/home" "$park/home"
fi
log "$CVE/$VARIANT: $TOOL exited $CODE after ${SECS}s, $(jq '.findings|length' "$CELL/findings.json") finding(s)"
