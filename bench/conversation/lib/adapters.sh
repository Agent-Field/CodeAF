#!/usr/bin/env bash
# adapters.sh — one module per harness, behind one contract.
#
# Everything this suite knows about a particular CLI lives here: isolation, the
# spelling of the pinned model, the effort rungs it really has, its print door,
# whether this suite can drive its interactive door, and where its receipts
# land. A scenario never names a harness; run.sh never names a flag.
#
# The flags were read off the installed binaries rather than assumed, and the
# behavioural claims (effort rungs, what a TUI prints while working) say where
# the observation came from. A flag that moved between versions looks exactly
# like a harness that ran and did nothing, which is why the versions are on
# every row.
#
#   arm       version seen   print door             interactive door
#   aforge    (this build)   chat --once            TUI over tmux
#   omp       18.1.2         -p --mode json         TUI over tmux
#   pi        0.84.2         -p --mode json         TUI over tmux
#   opencode  1.17.15        run --format json      none this suite drives
#
# The contract, in call order:
#
#   arm_known / arm_bin / arm_version
#   arm_model_arg <arm>                 this arm's spelling of the pinned id
#   arm_pin_check <arm>                 can this arm pin that exact id
#   arm_role_pin <arm>                  can every auxiliary call be pinned too
#   arm_effort <arm> <effort>           ARM_EFFORT_* — sent, supported, note
#   arm_isolate <arm> <cell-dir>        ARM_ENV, ARM_STATE_DIR, ARM_ISOLATION
#   arm_print_argv <arm> <work> <text>  ARGV for one message, non-interactive
#   arm_tui_argv <arm> <work> <cap-s>   ARGV for the interactive door, or 1
#   arm_receipt_kind / arm_receipt_path which reader in receipts.py, and where

CONV_ADAPTERS_LIB="${CONV_ADAPTERS_LIB:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"

AFORGE_BIN="${AFORGE_BIN:-}"
if [ -z "$AFORGE_BIN" ]; then
  if [ -x "$CONV_REPO_ROOT/bin/aforge" ]; then
    AFORGE_BIN="$CONV_REPO_ROOT/bin/aforge"
  else
    AFORGE_BIN="$(command -v aforge || echo "$CONV_REPO_ROOT/bin/aforge")"
  fi
fi
OMP_BIN="${OMP_BIN:-omp}"
PI_BIN="${PI_BIN:-pi}"
OPENCODE_BIN="${OPENCODE_BIN:-opencode}"

CONV_ALL_ARMS="aforge omp pi opencode"

arm_known() {
  case "$1" in aforge|omp|pi|opencode) return 0 ;; *) return 1 ;; esac
}

arm_bin() {
  case "$1" in
    aforge)   [ -x "$AFORGE_BIN" ] && printf '%s' "$AFORGE_BIN" ;;
    omp)      command -v "$OMP_BIN" 2>/dev/null ;;
    pi)       command -v "$PI_BIN" 2>/dev/null ;;
    opencode) command -v "$OPENCODE_BIN" 2>/dev/null ;;
  esac
}

# arm_version records what was actually measured. A benchmark row without it is
# a row nobody can reproduce: all three peers move their flags between versions.
arm_version() {
  local arm="$1" bin
  bin="$(arm_bin "$arm")" || true
  [ -n "$bin" ] || { printf 'not-installed'; return; }
  case "$arm" in
    aforge)   "$bin" --version 2>/dev/null | head -1 ;;
    omp)      "$bin" --version 2>/dev/null | head -1 ;;
    pi)       "$bin" --version 2>/dev/null | tail -1 ;;
    opencode) "$bin" --version 2>/dev/null | tail -1 ;;
  esac
}

# ── the model pin ───────────────────────────────────────────────────────────

# arm_model_arg is the pinned id in this arm's own spelling. The id is one
# thing; how each CLI wants to be told about it is another, and conflating them
# is how a grid ends up comparing two models.
arm_model_arg() {
  case "$1" in
    aforge|pi)    printf '%s' "$CONV_MODEL" ;;
    omp)          [ "${ARM_GUARD:-no}" = "yes" ] && printf 'guard/%s' "$CONV_MODEL" \
                                                 || printf 'openrouter/%s' "$CONV_MODEL" ;;
    opencode)     printf 'openrouter/%s' "$CONV_MODEL" ;;
  esac
}

# arm_provider_name is the provider pi is told to use. Wired through the guard,
# that is the guard's own provider entry rather than openrouter directly.
arm_provider_name() {
  [ "${ARM_GUARD:-no}" = "yes" ] && printf 'guard' || printf 'openrouter'
}

# arm_pin_check asks the arm's own catalog whether the exact id exists, before
# any spend. A miss is a skipped arm carrying its reason, never a run on a
# neighbouring model: substring-matching `deepseek-v4-flash` would happily
# accept `deepseek-v4-flash-0731`'s sibling and put both rows in one table.
ARM_PIN_NOTE=""
arm_pin_check() {
  local arm="$1" bin
  ARM_PIN_NOTE=""
  bin="$(arm_bin "$arm")" || true
  if [ -z "$bin" ]; then ARM_PIN_NOTE="$arm not installed"; return 1; fi
  # The catalog is asked in the environment and state root the cell will use.
  # Asking in the operator's instead answers a different question: on this
  # machine pi's default profile lists no openrouter models at all, while a
  # fresh PI_CODING_AGENT_DIR does.
  local -a ask=()
  [ "${#CHILD_ENV[@]}" -gt 0 ] && ask=("${CHILD_ENV[@]}")

  # A search pattern, not the answer. pi and omp both fuzzy-match, and the full
  # id with its provider prefix matches fewer rows than its last segment does —
  # so the query is deliberately loose and the MATCH below is exact.
  local pattern="${CONV_MODEL##*/}"

  case "$arm" in
    pi)
      # One row per model, provider first and id second. Both must match: the
      # arm is run with --provider openrouter, so another provider's row
      # carrying the same id is not evidence that this arm can pin it.
      if "${ask[@]}" "$bin" --list-models "$pattern" 2>/dev/null |
           awk -v prov="$(arm_provider_name)" -v want="$CONV_MODEL" \
               'NF >= 2 && $1 == prov && $2 == want { hit = 1 } END { exit !hit }'; then
        ARM_PIN_NOTE="pi catalog: exact ($(arm_provider_name)/$CONV_MODEL)"
        return 0
      fi
      ARM_PIN_NOTE="pi cannot pin $(arm_provider_name)/$CONV_MODEL exactly"
      return 1
      ;;
    omp)
      # omp's catalog carries the selector it accepts on --model, which is
      # exactly what this suite passes. Comparing selectors compares the thing
      # that will be sent rather than a name that happens to look similar.
      if "${ask[@]}" "$bin" models find "$pattern" --json 2>/dev/null |
           CONV_WANT="$(arm_model_arg omp)" python3 -c '
import json, os, sys
want = os.environ["CONV_WANT"]
try:
    blob = json.load(sys.stdin)
except Exception:
    sys.exit(1)
models = blob.get("models") if isinstance(blob, dict) else blob
for model in models or []:
    if not isinstance(model, dict):
        continue
    selector = model.get("selector") or "/".join(x for x in (model.get("provider"), model.get("id")) if x)
    if selector == want:
        sys.exit(0)
sys.exit(1)
'; then
        ARM_PIN_NOTE="omp catalog: exact ($(arm_model_arg omp))"
        return 0
      fi
      ARM_PIN_NOTE="omp cannot pin $(arm_model_arg omp) exactly"
      return 1
      ;;
    aforge)
      # aforge resolves its own catalog at call time and has no offline
      # "does this id exist" query that costs nothing. The pin is therefore
      # enforced after the fact instead, on the ids in the run's own receipts
      # (allowlist.sh), which is the stronger check anyway: it sees the roles
      # and the fallbacks, and a catalog query never does.
      ARM_PIN_NOTE="aforge: pin enforced on receipts, not by catalog query"
      return 0
      ;;
    opencode)
      ARM_PIN_NOTE="opencode: catalog not queried; receipts do not name the billed model"
      return 0
      ;;
  esac
  return 1
}

# ── pinning the auxiliary calls, before any of them are billed ──────────────
#
# A session is not one call. Titles, summaries, planning and "smol" helper roles
# are calls too, and by default they resolve through each CLI's own settings —
# which is how a run that named one model bills another. Checking the receipts
# afterwards finds that, but only after the money is gone, so the arms are
# separated here into those that can be pinned in advance and those that cannot:
#
#   aforge  yes — --one-model settles every text call on the session model.
#   omp     yes — --smol/--slow/--plan take a model each (18.1.2 --help), so the
#           three roles that would otherwise float are named explicitly.
#   pi      unverified — 0.84.2 --help documents no way to pin auxiliary roles.
#   opencode unverified — no documented role pins, and its events do not even
#           name the billed model afterwards.
#
# run.sh refuses to spend on an unverified arm unless the caller says otherwise
# (--role-pin off), because a hopeful paid call is exactly what the policy is
# there to prevent.
ARM_ROLE_FLAGS=()
ARM_ROLE_PIN=""
ARM_ROLE_NOTE=""
arm_role_pin() {
  local arm="$1" model; model="$(arm_model_arg "$arm")"
  ARM_ROLE_FLAGS=()
  case "$arm" in
    aforge)
      ARM_ROLE_PIN="yes"; ARM_ROLE_NOTE="--one-model pins every text call"
      ;;
    omp)
      ARM_ROLE_FLAGS=(--smol "$model" --slow "$model" --plan "$model")
      ARM_ROLE_PIN="yes"; ARM_ROLE_NOTE="--smol/--slow/--plan pinned to the session model"
      ;;
    pi)
      ARM_ROLE_PIN="unverified"; ARM_ROLE_NOTE="pi 0.84.2 documents no auxiliary-role pin"
      ;;
    opencode)
      ARM_ROLE_PIN="unverified"; ARM_ROLE_NOTE="opencode documents no role pin and its events name no model"
      ;;
  esac
  [ "$ARM_ROLE_PIN" = "yes" ]
}

# ── the ambient machine, and how much of it can be turned off ───────────────
#
# A harness that loads the machine's skills, extensions and MCP servers is not
# running the task it was given: it is running that task plus whatever the
# person who owns the machine installed. A live pane in this lane showed omp
# mounting MCP tools and failing one of them from `~/.claude.json`, and pi
# listing thirty-odd skills from `~/.agents/skills` — extra tools, extra system
# prompt, extra latency, and none of it the same on two machines.
#
# ONLY DOCUMENTED FLAGS ARE USED. What each CLI's own --help offers is taken;
# what it does not offer is REPORTED rather than worked around, because a
# benchmark that edits somebody's dotfiles to look fair has stopped measuring
# the thing people run.
#
#   omp 18.1.2   --no-skills --no-extensions --no-rules
#                (no flag for user-level MCP from ~/.claude.json; its config
#                 offers mcp.enableProjectConfig, which covers the project file
#                 only — recorded as a known difference)
#   pi 0.84.2    --no-skills --no-extensions
#   aforge       nothing needed: AFORGE_HOME moves the whole state root, so a
#                cell starts with no ambient skills or extensions at all
ARM_BASELINE_FLAGS=()
ARM_BASELINE_NOTE=""
arm_baseline() {
  ARM_BASELINE_FLAGS=()
  case "$1" in
    omp)
      ARM_BASELINE_FLAGS=(--no-skills --no-extensions --no-rules)
      ARM_BASELINE_NOTE="skills/extensions/rules off; user MCP from ~/.claude.json cannot be disabled by any documented flag and is still loaded"
      ;;
    pi)
      ARM_BASELINE_FLAGS=(--no-skills --no-extensions)
      ARM_BASELINE_NOTE="skills/extensions off"
      ;;
    aforge)
      ARM_BASELINE_NOTE="clean by isolation: AFORGE_HOME is this cell's own"
      ;;
    opencode)
      ARM_BASELINE_NOTE="unverified: no documented discovery switches read for opencode"
      ;;
  esac
}

# ── effort ──────────────────────────────────────────────────────────────────
#
# The four CLIs spell reasoning effort four ways and do not offer the same
# rungs. Asking for one the arm does not have is a mismatch to be REPORTED,
# never a substitution to be made quietly: a row that silently ran a rung above
# the others is the most flattering possible lie about cost.
#
# Levels are as printed by --help on the versions in the table above:
#   aforge    off low medium high
#   omp       off minimal low medium high xhigh max auto
#   pi        off minimal low high xhigh max          (no medium)
#   opencode  --variant <provider-specific>           (no enumerated list)
ARM_EFFORT_FLAGS=()
ARM_EFFORT_SENT=""
ARM_EFFORT_SUPPORTED=""
ARM_EFFORT_NOTE=""
arm_effort() {
  local arm="$1" want="$2" levels=""
  ARM_EFFORT_FLAGS=()
  ARM_EFFORT_SENT=""
  ARM_EFFORT_SUPPORTED="no"
  ARM_EFFORT_NOTE=""
  case "$arm" in
    aforge)   levels="off low medium high" ;;
    omp)      levels="off minimal low medium high xhigh max auto" ;;
    pi)       levels="off minimal low high xhigh max" ;;
    opencode) levels="" ;;
  esac
  if [ "$arm" = "opencode" ]; then
    # opencode takes a provider-specific variant string and enumerates nothing,
    # so what it does with a level cannot be verified from here.
    ARM_EFFORT_FLAGS=(--variant "$want")
    ARM_EFFORT_SENT="$want"
    ARM_EFFORT_SUPPORTED="unverified"
    ARM_EFFORT_NOTE="opencode --variant is provider-specific and unenumerated"
    return 0
  fi
  case " $levels " in
    *" $want "*)
      ARM_EFFORT_SENT="$want"
      ARM_EFFORT_SUPPORTED="yes"
      case "$arm" in
        aforge) ARM_EFFORT_FLAGS=(--reasoning "$want") ;;
        omp|pi) ARM_EFFORT_FLAGS=(--thinking "$want") ;;
      esac
      return 0
      ;;
  esac
  # The rung does not exist on this arm. Nothing is sent, and the cell carries
  # the mismatch so that no comparison is drawn from it.
  ARM_EFFORT_SENT="none"
  ARM_EFFORT_SUPPORTED="no"
  ARM_EFFORT_NOTE="$arm has no '$want' rung (has: $levels)"
  return 1
}

# ── isolation ───────────────────────────────────────────────────────────────
#
# A cell gets its own state root so that four cells never share one store, and
# so that a run never writes into the operator's own history. Only the api key
# is carried over from the person's environment; nothing here defines one.
ARM_ENV=()
ARM_STATE_DIR=""
ARM_ISOLATION=""
ARM_CLEANUP_PATH=""
arm_isolate() {
  local arm="$1" cell="$2"
  ARM_ENV=()
  ARM_CLEANUP_PATH=""
  ARM_STATE_DIR="$cell/state"
  mkdir -p "$ARM_STATE_DIR"
  case "$arm" in
    aforge)
      ARM_ENV=("AFORGE_HOME=$ARM_STATE_DIR/aforge-home")
      mkdir -p "$ARM_STATE_DIR/aforge-home"
      ARM_ISOLATION="full: AFORGE_HOME moves the whole state root"
      ;;
    omp)
      # omp's documented isolation is a named profile, and profiles live under
      # the operator's ~/.omp/profiles rather than under the cell. Two rules
      # follow, and both are about not touching somebody else's state:
      #
      #   the name is this run's, and is validated. A profile name reaches the
      #   filesystem as a path component, so anything but [A-Za-z0-9._-] is
      #   refused rather than joined into a path.
      #
      #   only a profile this cell CREATED is ever removed. An existing
      #   directory — the operator's own `work` profile, or a second run's — is
      #   left alone, and a caller who names one explicitly gets it used as-is
      #   with no cleanup and no seeding.
      local profile="${CONV_OMP_PROFILE:-afconv-$CONV_RUN_ID-$(basename "$cell")}"
      case "$profile" in
        *[!A-Za-z0-9._-]*|""|.|..)
          conv_warn "refusing an omp profile name that is not a plain path component: $profile"
          ARM_ISOLATION="refused: invalid profile name"
          return 1
          ;;
      esac
      local profile_root="$HOME/.omp/profiles/$profile"
      if [ -e "$profile_root" ]; then
        # A profile this run did not create carries settings this run did not
        # write — including model roles. It is neither used nor touched.
        conv_warn "omp profile $profile already exists; refusing to run inside settings this run did not write"
        ARM_ISOLATION="refused: omp profile $profile already exists (left untouched)"
        return 1
      else
        # A fresh profile opens omp's five-step setup wizard and the TUI never
        # reaches a composer (observed on 18.1.2), so the one key that says
        # setup is done is written. Only this branch takes ownership.
        mkdir -p "$profile_root/agent"
        printf 'setupVersion: 2\n' > "$profile_root/agent/config.yml"
        ARM_CLEANUP_PATH="$profile_root"
        CONV_OMP_PROFILE_ACTIVE="$profile"
        ARM_ISOLATION="omp profile $profile created by this run under \$HOME/.omp/profiles"
      fi
      ARM_ENV=("OMP_PROFILE=$profile")
      mkdir -p "$ARM_STATE_DIR/omp-sessions"
      ;;
    pi)
      ARM_ENV=("PI_CODING_AGENT_DIR=$ARM_STATE_DIR/pi-home")
      mkdir -p "$ARM_STATE_DIR/pi-home" "$ARM_STATE_DIR/pi-sessions"
      ARM_ISOLATION="full: PI_CODING_AGENT_DIR plus --session-dir"
      ;;
    opencode)
      mkdir -p "$ARM_STATE_DIR/oc-data" "$ARM_STATE_DIR/oc-config" "$ARM_STATE_DIR/oc-cache"
      ARM_ENV=("XDG_DATA_HOME=$ARM_STATE_DIR/oc-data"
               "XDG_CONFIG_HOME=$ARM_STATE_DIR/oc-config"
               "XDG_CACHE_HOME=$ARM_STATE_DIR/oc-cache")
      # opencode's --help documents no state-root variable, so this is the XDG
      # convention applied hopefully rather than a documented guarantee. It is
      # recorded as declared-unverified rather than claimed as isolation.
      ARM_ISOLATION="declared-unverified: XDG_* only, no documented state root"
      ;;
  esac
}

# ── the print door ──────────────────────────────────────────────────────────
#
# One message in, one reply out, nobody watching. This is a real door and the
# suite measures it — but it is NOT the interactive door and no row from it may
# be read as one. A benchmark that runs `--print` and calls the result
# conversation is measuring a different product than the one people use.
ARGV=()
arm_print_argv() {
  local arm="$1" work="$2" text="$3"
  local model; model="$(arm_model_arg "$arm")"
  case "$arm" in
    aforge)
      # --one-model is not optional: without it a chat session resolves titles,
      # reflexes and other auxiliary calls through role pins that this run never
      # named, and the open-model law would be enforced against a machine's
      # profile rather than against this run. --yolo because nobody is watching:
      # consent is refused rather than assumed, and a cell without it changes no
      # files while looking healthy.
      #
      # --no-host is deliberately NOT passed. A conversation is hosted by
      # default, so a benchmark that opted out would be measuring a path people
      # do not use. Each cell gets its own workspace, so the host it starts is
      # its own, and run.sh stops that one host when the cell ends.
      ARGV=("$AFORGE_BIN" chat --once "$text"
            --model "$model" --one-model --yolo "${ARM_EFFORT_FLAGS[@]}")
      ;;
    omp)
      ARGV=("$OMP_BIN" -p --mode json --model "$model" --cwd "$work"
            --session-dir "$ARM_STATE_DIR/omp-sessions" --auto-approve
            "${ARM_BASELINE_FLAGS[@]}" "${ARM_ROLE_FLAGS[@]}" "${ARM_EFFORT_FLAGS[@]}" "$text")
      ;;
    pi)
      # pi has no --cwd: it works in the directory it is started in, so run.sh
      # starts it inside the fixture.
      ARGV=("$PI_BIN" -p --mode json --provider "$(arm_provider_name)" --model "$model"
            --session-dir "$ARM_STATE_DIR/pi-sessions" "${ARM_BASELINE_FLAGS[@]}"
            "${ARM_EFFORT_FLAGS[@]}" "$text")
      ;;
    opencode)
      ARGV=("$OPENCODE_BIN" run --format json -m "$model" --dir "$work" --auto
            "${ARM_EFFORT_FLAGS[@]}" "$text")
      ;;
    *) return 1 ;;
  esac
}

# arm_print_cwd says where the print door must be launched from. Getting this
# wrong is the failure bench/README.md warns about: a zero exit, zero changed
# files, and a row that looks exactly like a real DNF.
arm_print_cwd() {
  case "$1" in
    omp|opencode) printf '%s' "$2" ;;   # told with a flag, but harmless to start there too
    *)            printf '%s' "$2" ;;   # aforge chat and pi both take the process's directory
  esac
}

# ── the interactive door ────────────────────────────────────────────────────
#
# A person's conversation happens in a terminal: the binary draws a screen, they
# type into it, and they can type again while it is still working. That is the
# thing this suite calls the interactive door, and it is driven through tmux —
# a real terminal, real keystrokes, real bracketed paste.
#
# An arm has an interactive door here only if this suite can tell, from the
# screen alone, when it is working and when it is not. Those markers are stated
# below with where they came from. An arm without calibrated markers is
# UNSUPPORTED and its interactive cells are recorded as such: not run, not
# passed, and excluded from every claim.
ARM_READY_RE=""
ARM_BUSY_RE=""
ARM_ASK_RE=""
ARM_DOOR_NOTE=""
arm_tui_argv() {
  local arm="$1" work="$2" cap="$3"
  local model; model="$(arm_model_arg "$arm")"
  ARM_READY_RE=""; ARM_BUSY_RE=""; ARM_ASK_RE=""; ARM_DOOR_NOTE=""
  case "$arm" in
    aforge)
      # Markers read off the CURRENT renderer rather than copied from an older
      # battery. internal/tui3/render.go's stateWord ends the status row with
      # the run state — "idle", "working", "interrupted" — or with waitingWord
      # ("waiting · your call") when consent is pending, and the rail counts
      # tasks separately ("1 running"). tui3's own chrome_test asserts a FRESH
      # screen's status row already says "idle", which is what makes it usable
      # as "the composer is up" before anything has been typed.
      # --max-hours is set inside the rig's own cap so the session ends on its
      # own law and writes its ending before the driver stops watching.
      local hours; hours="$(python3 -c "print(round(max($cap - 60, 60) / 3600, 4))")"
      ARM_READY_RE='· idle'
      ARM_BUSY_RE='(· (working|interrupted)|[0-9]+ running)'
      ARM_ASK_RE='waiting · your call'
      ARM_DOOR_NOTE='markers from internal/tui3/render.go (stateWord, waitingWord), matched against a live pane in /private/tmp/af-conversation-ops/host-live-01'
      # Hosted, like any other conversation: the interactive door is the one
      # place this suite can measure the product's actual default, and passing
      # --no-host here would quietly measure something else. The cell's own
      # host is stopped by name when the cell ends (run.sh: arm_host_stop).
      ARGV=("$AFORGE_BIN" chat --model "$model" --one-model --yolo
            --max-cost "${CONV_MAX_COST:-1}" --max-hours "$hours" "${ARM_EFFORT_FLAGS[@]}")
      ;;
    pi)
      # Calibrated on pi 0.84.2 in a tmux pane: while a tool runs the transcript
      # carries "⠙ Working..." and an "Elapsed 8.0s" line, and both are gone
      # when the turn ends. The status bar always carries the provider and the
      # model, which is also what makes "the composer is up" legible.
      # Only the spinner line counts as busy. "Elapsed 8.0s" is left in the
      # transcript by a finished tool call as well as a running one, so reading
      # it as busy would leave the driver waiting for an idle that has already
      # happened.
      #
      # THE PROVIDER NAME IS DERIVED, NOT HARDCODED. The status bar names
      # whichever provider is in use, so a rig that wired pi to the guard and
      # then waited for "(openrouter)" waits forever: a live pane on a guarded
      # run reads "(guard) deepseek/deepseek-v4-flash-0731 • low"
      # (/private/tmp/af-conversation-ops/live-interactive-peers-01/followup-while-working-pi/screen.txt).
      ARM_READY_RE="\\($(arm_provider_name)\\)"
      ARM_BUSY_RE='Working\.\.\.'
      ARM_ASK_RE='(\[y\]|approve|Allow\?)'
      ARM_DOOR_NOTE="markers calibrated on pi 0.84.2 (bench/conversation lane, live pane capture); provider segment derived as ($(arm_provider_name))"
      ARGV=("$PI_BIN" --provider "$(arm_provider_name)" --model "$model"
            --session-dir "$ARM_STATE_DIR/pi-sessions" "${ARM_BASELINE_FLAGS[@]}"
            "${ARM_EFFORT_FLAGS[@]}")
      ;;
    omp)
      # Calibrated on omp 18.1.2 the same way: the status line begins with the
      # "π  >" prompt glyph when idle and with a braille spinner and an elapsed
      # count ("⠦ 12s  >") while a turn or a tool is in flight; "⚙ N" appears
      # while N subagents are running.
      ARM_READY_RE='π  >'
      ARM_BUSY_RE='([⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏] [0-9]+s|⚙ [0-9]+)'
      ARM_ASK_RE='(\[y\]|approve|Allow\?)'
      ARM_DOOR_NOTE='markers calibrated on omp 18.1.2 (bench/conversation lane, live pane capture)'
      ARGV=("$OMP_BIN" --model "$model" --session-dir "$ARM_STATE_DIR/omp-sessions"
            --auto-approve --max-time "$cap" "${ARM_BASELINE_FLAGS[@]}"
            "${ARM_ROLE_FLAGS[@]}" "${ARM_EFFORT_FLAGS[@]}")
      ;;
    opencode)
      ARM_DOOR_NOTE='no calibrated screen markers for opencode 1.17.15 — interactive door unsupported here'
      return 1
      ;;
    *) return 1 ;;
  esac
  return 0
}

# arm_receipt_kind names the reader in receipts.py that can read this arm's own
# account of what it spent.
arm_receipt_kind() {
  case "$1" in
    aforge)   printf 'aforge-home' ;;
    omp|pi)   printf 'pi-events' ;;
    opencode) printf 'opencode-events' ;;
  esac
}

# arm_receipt_path is where that reader must look, which depends on the door.
#
# Through the print door pi, omp and opencode stream their events to stdout and
# the captured log is the receipt. Through the interactive door they stream
# nothing: the session file in their session directory is all there is, and this
# suite reads it opportunistically — the file is JSON Lines and carries
# assistant messages, but its exact schema has not been verified here, so a
# cell whose cost cannot be read comes back `unknown` and not-comparable rather
# than zero. aforge is the same either way: the home is the witness.
arm_receipt_path() {
  local arm="$1" cell="$2" door="${3:-print}"
  case "$arm" in
    aforge) printf '%s' "$cell/state/aforge-home" ;;
    pi)     [ "$door" = "interactive" ] && printf '%s' "$cell/state/pi-sessions" || printf '%s' "$cell/stdout.log" ;;
    omp)    [ "$door" = "interactive" ] && printf '%s' "$cell/state/omp-sessions" || printf '%s' "$cell/stdout.log" ;;
    *)      printf '%s' "$cell/stdout.log" ;;
  esac
}
