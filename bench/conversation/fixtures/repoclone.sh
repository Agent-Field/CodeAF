#!/usr/bin/env bash
# repoclone — the workspace for the asks that are about THIS repository.
#
# WHAT SHAPE THIS REPRODUCES, and why each part of it is load-bearing. On
# 2026-09-11 one paragraph of UI request cost seventeen minutes, sixty-two tool
# calls and forty rounds, and then spilled into a worker task. Everything about
# that turn that was not the model is staged here:
#
#   a fresh clone       the harness has no warm memory of the tree, no prior
#                       conversation about it, and nothing in its state root.
#                       A second run against a workspace the first one explored
#                       measures the cache, not the build.
#   a pinned commit     both arms edit character-for-character the same source.
#                       A moving tip would let one arm answer an easier question.
#   cwd is NOT the repo the owner launched from their home directory and named
#                       the project in the message. Finding the folder is part
#                       of the turn — and on the night in question a large part.
#   the project by name the first message says "aforge-v2" and nothing else
#                       points at it, exactly as a person types it.
#
# The judge is the clone itself afterwards: what it builds, what it vets, what
# its own named test says, and what `git status` says changed. Nothing here
# reads the harness's account of what it did.

# The commit every cell of this battery works over. IT IS A LITERAL AND IT IS
# MEANT TO BE: a battery pinned to `origin/dev` would measure a different tree
# every week and its old rows would stop being comparable with its new ones.
# Moving it is a deliberate act that starts a new experiment — bump this, and
# say so in the campaign's id.
CONV_REPO_PIN="${CONV_REPO_PIN:-82624af79bb77415872a35371d27a10afcc2a8a1}"

# The nickname a person uses for the project, which is the only pointer the
# opening message carries.
CONV_REPO_NICKNAME="${CONV_REPO_NICKNAME:-aforge-v2}"

# Where the clone's objects come from. A local path by default — the checkout
# this rig sits in already has the commit, and cloning over a network would put
# somebody else's bandwidth inside a wall-time measurement.
CONV_REPO_SOURCE="${CONV_REPO_SOURCE:-$CONV_REPO_ROOT}"

# fixture_repoclone lays out one cell's workspace:
#
#   <work>/                    the launch directory — NOT a repository
#   <work>/<nickname>/         the shallow clone, detached at the pin
#
#   fixture_repoclone <work> <judge>
fixture_repoclone() {
  local work="$1" judge="$2" clone="$1/$CONV_REPO_NICKNAME"
  mkdir -p "$work" "$judge"

  if ! git -C "$CONV_REPO_SOURCE" rev-parse --verify --quiet "$CONV_REPO_PIN^{commit}" >/dev/null; then
    conv_warn "the pinned commit $CONV_REPO_PIN is not in $CONV_REPO_SOURCE — fetch it first"
    return 1
  fi

  # A one-commit clone, which is what a person gets when they clone to look at
  # something: `git log` has one entry and the history is not the question.
  # Fetching the commit by its own id rather than a branch is what keeps the
  # workspace identical for every cell however the branch has moved since.
  git init -q "$clone" || return 1
  git -C "$clone" fetch -q --depth 1 "$CONV_REPO_SOURCE" "$CONV_REPO_PIN" || return 1
  git -C "$clone" checkout -q --detach FETCH_HEAD || return 1
  # A person's clone has a name for where it came from, and a harness that runs
  # `git status` in a repository with no remote sees a shape people do not have.
  git -C "$clone" remote add origin "$CONV_REPO_SOURCE" 2>/dev/null

  # The answer key for this workspace is what it looked like before anybody
  # touched it. It lives beside the cell, never inside the workspace.
  git -C "$clone" rev-parse HEAD > "$judge/pin.txt"
  git -C "$clone" status --porcelain > "$judge/porcelain-before.txt"
  printf '%s\n' "$clone" > "$judge/clone-path.txt"
}

# repoclone_path prints the clone inside a cell's workspace.
repoclone_path() { printf '%s' "$1/$CONV_REPO_NICKNAME"; }

# ── the judge ───────────────────────────────────────────────────────────────
#
# Every one of these reads the CLONE. A harness's reply saying it added the
# hover is not evidence that the hover is there, and the failure that makes a
# benchmark worthless is the one where a fluent summary of work that did not
# happen is scored as work that did.

# repoclone_changed prints the paths git says are new or modified.
repoclone_changed() {
  git -C "$(repoclone_path "$1")" status --porcelain 2>/dev/null |
    sed 's/^...//' | sed 's/.* -> //'
}

# repoclone_measure_changed puts the count on the row. A turn that changed
# nothing is the outcome the night in question actually produced twice, and it
# has to be visible as a number rather than inferred from a failed check.
repoclone_measure_changed() {
  local work="$1" count
  count="$(repoclone_changed "$work" | grep -c . | tr -d '[:space:]')"
  measure "files_changed" "${count:-0}"
  note "changed=$(repoclone_changed "$work" | tr '\n' ' ' | tr ',' ';')"
}

# repoclone_go runs one command of the clone's own toolchain and puts its exit
# code in a column. The cap is this suite's, not the toolchain's: a wedged
# compiler must not hold a grid open, and a judge that timed out is recorded as
# the non-zero it is rather than waited out.
#
#   repoclone_go <work> <cell> <column> <log-name> <description> <command...>
repoclone_go() {
  local work="$1" cell="$2" column="$3" log="$4" description="$5"; shift 5
  local clone code
  clone="$(repoclone_path "$work")"
  if [ -n "$TIMEOUT_BIN" ]; then
    ( cd "$clone" && "$TIMEOUT_BIN" "${CONV_JUDGE_CAP_S:-900}" "$@" ) > "$cell/$log" 2>&1
  else
    ( cd "$clone" && "$@" ) > "$cell/$log" 2>&1
  fi
  code=$?
  measure "$column" "$code"
  if [ "$code" = "0" ]; then
    pass "$description"
  else
    fail "$description — exit $code, see $log"
    note "$column-tail=$(tail -3 "$cell/$log" 2>/dev/null | tr '\n' ' ' | tr ',' ';')"
  fi
  return "$code"
}

# repoclone_task_worktree records whether the conversation spilled into work of
# its own INSIDE the workspace. A task in a git place gets a worktree under
# `.aforge-v3/tasks/`, so this is the same fact the home's journals carry, seen
# from the other side — and a cell where the two disagree is worth knowing about.
repoclone_task_worktree() {
  local clone; clone="$(repoclone_path "$1")"
  if [ -d "$clone/.aforge-v3/tasks" ] && [ -n "$(ls -A "$clone/.aforge-v3/tasks" 2>/dev/null)" ]; then
    record "task_worktree" "yes ($(ls "$clone/.aforge-v3/tasks" | tr '\n' ' '))"
  else
    record "task_worktree" "no"
  fi
}

# repoclone_ask_wired asks whether the thing that was ASKED FOR is in the
# source now, by reading only the files the arm chose to change.
#
# IT NAMES NO FILE. The request names a behaviour and the arm picks where that
# behaviour lives; a check that demanded `taskstable.go` would fail a correct
# answer written one door along, and scoring tidiness as correctness is how a
# benchmark starts measuring its own opinions. Every pattern has to be matched
# somewhere in the changed set — by one file or between them — and the file
# that matched each is recorded so a reader can see what the arm actually did.
#
#   repoclone_ask_wired <work> <description> <pattern> [more patterns...]
repoclone_ask_wired() {
  local work="$1" description="$2"; shift 2
  local clone changed pattern missing="" hits=""
  clone="$(repoclone_path "$work")"
  changed="$(repoclone_changed "$work")"
  if [ -z "$changed" ]; then
    measure "ask_wired" "no"
    fail "$description — the workspace is unchanged"
    return 1
  fi
  for pattern in "$@"; do
    local found=""
    local path
    while IFS= read -r path; do
      [ -n "$path" ] || continue
      [ -f "$clone/$path" ] || continue
      if grep -aqE -- "$pattern" "$clone/$path"; then found="$path"; break; fi
    done <<EOF
$changed
EOF
    if [ -n "$found" ]; then
      hits="$hits $pattern->$(basename "$found")"
    else
      missing="$missing $pattern"
    fi
  done
  note "ask_matches=$(printf '%s' "$hits" | tr ',' ';')"
  if [ -z "$missing" ]; then
    measure "ask_wired" "yes"
    pass "$description"
    return 0
  fi
  measure "ask_wired" "no"
  fail "$description — nothing the arm changed carries:$missing"
  return 1
}
