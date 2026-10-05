#!/usr/bin/env bash
# Fetch everything the rig reads that is not in this tree: the OpenSSF dataset,
# the OSV description of every CVE in the set (what the judge is shown), and
# DeepSource's judged and processed rows (what calibrate.sh replays and what the
# comparison table was built from). Idempotent; re-run to refresh.
#
#   bench/openssf-cve/fetch.sh
#
# Env: WORK, DATASET, SET (which CVEs get an OSV record; default deepsource),
#      DEEPSOURCE_REV (pin), OSV_JOBS (parallel OSV fetches, default 8).
set -uo pipefail
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
mkdir -p "$WORK"

# --- the dataset ---------------------------------------------------------------
if [ -d "$DATASET/.git" ]; then
  log "dataset: already at $DATASET, pulling"
  git -C "$DATASET" pull -q --ff-only >/dev/null 2>&1 || log "dataset: pull failed, keeping what is there"
else
  log "dataset: cloning $DATASET_URL"
  git clone -q --depth 1 "$DATASET_URL" "$DATASET" || { log "dataset: clone failed"; exit 1; }
fi
git -C "$DATASET" rev-parse HEAD > "$WORK/dataset.rev"
log "dataset: $(ls "$DATASET/CVEs" | wc -l | tr -d ' ') records at $(cut -c1-12 "$WORK/dataset.rev")"

# --- DeepSource's rows, pinned -------------------------------------------------
# judged-results carries their judge's TP/FP/TN/FN per row; processed carries
# the findings their judge saw. Both are what calibrate.sh needs: replay the
# findings through OUR judge, compare with THEIR verdicts.
DS="$WORK/deepsource"; mkdir -p "$DS/judged" "$DS/processed"
base="https://raw.githubusercontent.com/$DEEPSOURCE_REPO/$DEEPSOURCE_REV/benchmarks"
for t in $DEEPSOURCE_TOOLS; do
  for kind in judged processed; do
    src="$kind-results"; [ "$kind" = processed ] && src=processed
    if [ ! -s "$DS/$kind/$t.jsonl" ]; then
      curl -fsSL "$base/$src/$t.jsonl" -o "$DS/$kind/$t.jsonl" || log "deepsource: no $kind rows for $t"
    fi
  done
done
printf '%s\n' "$DEEPSOURCE_REV" > "$DS/rev"
log "deepsource: rows for $(ls "$DS/judged" | wc -l | tr -d ' ') tools at $(cut -c1-12 "$DS/rev")"

# The committed comparison table is derived from those files. Rebuild it and
# say so if the committed copy has drifted, rather than silently quoting one
# table and shipping another.
python3 "$RIG_DIR/comparison/build.py" "$DS" > "$WORK/deepsource-table.json"
if ! cmp -s "$WORK/deepsource-table.json" "$RIG_DIR/comparison/deepsource-2026-04.json"; then
  log "WARNING: comparison/deepsource-2026-04.json differs from a fresh build; see $WORK/deepsource-table.json"
fi

# --- OSV descriptions ---------------------------------------------------------
# The judge reads the CVE's prose from OSV, the same source DeepSource's judged
# rows cite (cve_explanation_source: osv). A record is fetched once.
OSV="$WORK/osv"; mkdir -p "$OSV"
set_rows | cut -f1 | sort -u > "$WORK/cves.txt"
need=0
while read -r cve; do [ -s "$OSV/$cve.json" ] || { need=$((need+1)); printf '%s\n' "$cve"; }; done < "$WORK/cves.txt" > "$WORK/osv-need.txt"
if [ "$need" -gt 0 ]; then
  log "osv: fetching $need records"
  python3 - "$OSV" "$WORK/osv-need.txt" "${OSV_JOBS:-8}" <<'PY'
import json, os, sys, urllib.request
from concurrent.futures import ThreadPoolExecutor
out, need, jobs = sys.argv[1], sys.argv[2], int(sys.argv[3])
cves = [l.strip() for l in open(need) if l.strip()]
def fetch(cve):
    try:
        with urllib.request.urlopen("https://api.osv.dev/v1/vulns/" + cve, timeout=30) as r:
            data = json.load(r)
        json.dump(data, open(os.path.join(out, cve + ".json"), "w"))
        return True
    except Exception as e:
        print("osv: %s: %s" % (cve, e), file=sys.stderr)
        return False
with ThreadPoolExecutor(jobs) as ex:
    list(ex.map(fetch, cves))
PY
fi
# OSV has no record for a few of these (CVE-2020-4066 and others answer 404).
# DeepSource's judged rows carry the prose their judge read for every row,
# with its source named; where OSV is silent that text becomes the record, and
# the record says where it came from.
python3 - "$OSV" "$WORK/cves.txt" "$DS/judged" <<'PY'
import glob, json, os, sys
out, cves, judged = sys.argv[1], [l.strip() for l in open(sys.argv[2]) if l.strip()], sys.argv[3]
have = {}
for path in glob.glob(os.path.join(judged, "*.jsonl")):
    for line in open(path):
        r = json.loads(line)
        if r.get("cve_explanation") and r["cve_id"] not in have:
            have[r["cve_id"]] = (r["cve_explanation"], r.get("cve_explanation_source"))
for cve in cves:
    p = os.path.join(out, cve + ".json")
    if os.path.exists(p) or cve not in have:
        continue
    text, source = have[cve]
    json.dump({"id": cve, "details": text, "source": "deepsource judged rows (%s)" % (source or "unknown")},
              open(p, "w"), indent=2)
    print("osv: %s taken from DeepSource's rows (%s)" % (cve, source), file=sys.stderr)
PY
missing="$(while read -r cve; do [ -s "$OSV/$cve.json" ] || printf '%s ' "$cve"; done < "$WORK/cves.txt")"
[ -z "$missing" ] && log "osv: every CVE in the set has a record" || log "osv: still missing: $missing"
