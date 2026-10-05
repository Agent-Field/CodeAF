#!/usr/bin/env bash
# Materialise the checkouts a run reads: for every CVE named (or every CVE in
# SET when none is), the repository at its vulnerable commit and at its fixed
# commit, each in two shapes, plus the truth record the judge reads.
#
#   bench/openssf-cve/prepare.sh [<cve> ...]
#
# Layout under $WORK/repos/<CVE>/<variant>/:
#   tree/   the whole repository at that commit, as one commit on `main`
#   pr/     the pull-request shape DeepSource reviewed: `main` is the same tree
#           WITHOUT the files the fix touched, `review` adds them back in full.
#           A tool reviewing `review` against `main` sees exactly what a
#           reviewer of their draft PR saw — the whole file, as new code.
#   pr-files.txt   the files that pull request adds
#   info.json      cve, variant, commit, weakness locations, CWEs, OSV prose.
#                  THE ANSWER LIVES HERE AND NOWHERE A TOOL CAN SEE: cell.sh
#                  hands a tool a copy of tree/ or pr/ and nothing else.
#
# The repository is cloned once, bare and blob-less, under $WORK/clones/; a
# commit the default fetch did not bring is fetched by hash. A repository that
# no longer exists (one of 186 on 2026-10-05) leaves unavailable.txt beside
# where its checkouts would be, and every run reports that row as unavailable
# rather than as a miss.
#
# Env: WORK, DATASET, SET, FORCE=1 (rebuild existing checkouts).
set -uo pipefail
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
[ -d "$DATASET/CVEs" ] || { log "no dataset at $DATASET — run fetch.sh first"; exit 1; }

GIT_ID=(-c user.name=bench -c user.email=bench@example.invalid -c commit.gpgsign=false)

clone_repo() { # <url> -> prints clone dir, or fails
  local url="$1" slug dir
  slug="$(repo_slug "$url")"; dir="$WORK/clones/$slug.git"
  if [ ! -d "$dir" ]; then
    mkdir -p "$WORK/clones"
    GIT_TERMINAL_PROMPT=0 git clone -q --bare --filter=blob:none "$url" "$dir" 2>"$dir.err" || { rm -rf "$dir"; return 1; }
    rm -f "$dir.err"
  fi
  printf '%s' "$dir"
}

have_commit() { git -C "$1" cat-file -e "$2^{commit}" 2>/dev/null; }
ensure_commit() { # <clone> <sha>
  have_commit "$1" "$2" && return 0
  GIT_TERMINAL_PROMPT=0 git -C "$1" fetch -q origin "$2" 2>/dev/null
  have_commit "$1" "$2"
}

# The files the pull request adds: everything the fix touched, plus the
# weakness locations themselves (the fix for windows-build-tools' CVE-2017-16003
# is in constants.js while the weakness is recorded in download.js — both are
# in the review). Only files that exist in THIS variant's tree are listed.
pr_files() { # <clone> <pre> <post> <cve> <tree-dir>
  local clone="$1" pre="$2" post="$3" cve="$4" tree="$5" f
  { git -C "$clone" diff --name-only "$pre" "$post" 2>/dev/null
    cve_field "$cve" '.prePatch.weaknesses[].location.file'; } | sort -u | while read -r f; do
    [ -n "$f" ] && [ -f "$tree/$f" ] && printf '%s\n' "$f"
  done
}

build_variant() { # <cve> <variant> <clone> <sha> <other-sha>
  local cve="$1" variant="$2" clone="$3" sha="$4" other="$5"
  local dest="$WORK/repos/$cve/$variant" tree pr pre post
  if [ -s "$dest/info.json" ] && [ "${FORCE:-0}" != 1 ]; then return 0; fi
  rm -rf "$dest"; mkdir -p "$dest"
  tree="$dest/tree"; pr="$dest/pr"

  mkdir -p "$tree"
  git -C "$clone" archive "$sha" | tar -x -C "$tree" || { log "$cve/$variant: archive failed"; rm -rf "$dest"; return 1; }
  git -C "$tree" "${GIT_ID[@]}" init -q -b main
  git -C "$tree" "${GIT_ID[@]}" add -A >/dev/null 2>&1
  git -C "$tree" "${GIT_ID[@]}" commit -q -m "$cve $variant ($sha)" >/dev/null 2>&1

  if [ "$variant" = unfixed ]; then pre="$sha"; post="$other"; else pre="$other"; post="$sha"; fi
  pr_files "$clone" "$pre" "$post" "$cve" "$tree" > "$dest/pr-files.txt"

  # The PR shape: copy the tree, take the PR files out of main, add them back
  # on review. Paths are moved through a side directory so a file inside a
  # directory the PR also creates survives the round trip.
  mkdir -p "$pr"
  (cd "$tree" && tar -c --exclude=.git .) | tar -x -C "$pr"
  mkdir -p "$dest/.held"
  while read -r f; do
    [ -n "$f" ] || continue
    mkdir -p "$dest/.held/$(dirname "$f")"; mv "$pr/$f" "$dest/.held/$f"
  done < "$dest/pr-files.txt"
  git -C "$pr" "${GIT_ID[@]}" init -q -b main
  git -C "$pr" "${GIT_ID[@]}" add -A >/dev/null 2>&1
  git -C "$pr" "${GIT_ID[@]}" commit -q --allow-empty -m "base" >/dev/null 2>&1
  git -C "$pr" "${GIT_ID[@]}" checkout -q -b review
  while read -r f; do
    [ -n "$f" ] || continue
    mkdir -p "$pr/$(dirname "$f")"; mv "$dest/.held/$f" "$pr/$f"
  done < "$dest/pr-files.txt"
  rm -rf "$dest/.held"
  git -C "$pr" "${GIT_ID[@]}" add -A >/dev/null 2>&1
  git -C "$pr" "${GIT_ID[@]}" commit -q --allow-empty -m "add $(wc -l < "$dest/pr-files.txt" | tr -d ' ') file(s)" >/dev/null 2>&1

  python3 - "$cve" "$variant" "$sha" "$other" "$(cve_json "$cve")" "$WORK/osv/$cve.json" "$dest" <<'PY'
import json, os, sys
cve, variant, sha, other, record, osv, dest = sys.argv[1:8]
r = json.load(open(record))
weak = [{"file": w["location"]["file"], "line": w["location"].get("line"),
         "explanation": w.get("explanation", "")} for w in r["prePatch"]["weaknesses"]]
prose = ""
if os.path.exists(osv):
    try:
        o = json.load(open(osv))
        prose = (o.get("details") or o.get("summary") or "").strip()
    except Exception:
        pass
files = [l.strip() for l in open(os.path.join(dest, "pr-files.txt")) if l.strip()]
present = [w for w in weak if os.path.exists(os.path.join(dest, "tree", w["file"]))]
json.dump({
    "cve": cve, "variant": variant, "repository": r["repository"], "commit": sha,
    "other_commit": other, "cwes": r.get("CWEs", []), "weaknesses": weak,
    "weakness_files_present": len(present), "pr_files": files, "osv_details": prose,
}, open(os.path.join(dest, "info.json"), "w"), indent=2)
PY
  log "$cve/$variant: ready ($(wc -l < "$dest/pr-files.txt" | tr -d ' ') PR file(s))"
}

prepare_cve() { # <cve>
  local cve="$1" url pre post clone
  [ -f "$(cve_json "$cve")" ] || { log "$cve: not in the dataset"; return 1; }
  url="$(cve_field "$cve" .repository)"; pre="$(cve_field "$cve" .prePatch.commit)"; post="$(cve_field "$cve" .postPatch.commit)"
  mkdir -p "$WORK/repos/$cve"
  if ! clone="$(clone_repo "$url")"; then
    printf 'repository unavailable: %s\n' "$url" > "$WORK/repos/$cve/unavailable.txt"
    log "$cve: repository unavailable ($url)"; return 1
  fi
  rm -f "$WORK/repos/$cve/unavailable.txt"
  for sha in "$pre" "$post"; do
    ensure_commit "$clone" "$sha" || { printf 'commit %s not fetchable from %s\n' "$sha" "$url" > "$WORK/repos/$cve/unavailable.txt"; log "$cve: commit $sha not fetchable"; return 1; }
  done
  build_variant "$cve" unfixed "$clone" "$pre" "$post"
  build_variant "$cve" fixed "$clone" "$post" "$pre"
}

if [ $# -gt 0 ]; then
  for cve in "$@"; do prepare_cve "$cve"; done
else
  set_rows | cut -f1 | sort -u | while read -r cve; do prepare_cve "$cve"; done
fi
