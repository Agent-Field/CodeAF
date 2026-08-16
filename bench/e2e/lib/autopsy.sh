#!/usr/bin/env bash
# autopsy.sh — reading a finished run out of the store it left behind.
#
# `aforge do --keep` prints "store kept at <dir>" on stderr and leaves a SQLite
# journal at <dir>/graph.db. Every question this battery asks about a run's
# *shape* — how many nodes, which route, did the leaves start together, what did
# it cost — is answered from that journal rather than from the run's own prose,
# for the same reason bench/run.sh reads pytest instead of the harness's summary:
# the thing being measured does not get to grade itself.
#
# The journal is WAL-mode, so every read here opens it read-only via a file: URI.
# Opening it read-write would checkpoint the WAL and mutate the evidence.
#
# Sourced by run.sh. Every function takes the db path as its first argument.

# store_dir_of extracts the kept store's directory from a captured stderr log.
# The line is written only when the store was ephemeral and --keep was given.
store_dir_of() {
  local stderr_log="$1"
  [ -f "$stderr_log" ] || return 0
  grep -aoE '^store kept at .*' "$stderr_log" | tail -1 | sed -E 's#^store kept at ##'
}

# q runs one query and prints the raw result. An unreadable store is an empty
# answer, never a crash: a cell whose run died before it wrote a journal must
# still be able to record that fact in the CSV.
#
# The read-only open is tried first, and it is tried through a file: URI because
# that is the only way to ask sqlite3 for one. It can legitimately fail: a
# WAL-mode database whose -wal and -shm sidecars are still present needs to
# recover them before it can answer anything, and recovery is a write. A store
# read in place immediately after the engine let go of it is exactly that case,
# and it fails by returning nothing rather than by erroring — which is how an
# autopsy silently reports a healthy run as zero nodes and no route.
#
# So a read-only open that comes back empty on a file that exists falls through
# to an ordinary one. Callers stow the store before reading it (see run.sh), so
# by then the copy belongs to the battery and checkpointing it costs nothing.
q() {
  local db="$1"; shift
  [ -n "$db" ] && [ -f "$db" ] || return 0
  local value
  value="$(sqlite3 "file:${db}?mode=ro" "$@" 2>/dev/null)"
  if [ -z "$value" ]; then
    value="$(sqlite3 "$db" "$@" 2>/dev/null)"
  fi
  printf '%s' "$value"
}

# num is q for a scalar that must be a number, defaulting to 0.
num() {
  local value
  value="$(q "$@")"
  echo "${value:-0}"
}

# ── shape ───────────────────────────────────────────────────────────────────

# node_count is how many work nodes the run created. The spine root is excluded:
# it is the job's own namespace row, not work, and counting it would make every
# single-leaf run read as two. This matches the "<n> nodes" figure `do` prints.
node_count() {
  num "$1" "SELECT count(*) FROM nodes WHERE parent_id IS NOT NULL;"
}

# leaf_count is the work nodes that ran rather than merely existed.
leaf_count() {
  num "$1" "SELECT count(*) FROM nodes WHERE parent_id IS NOT NULL AND started_at IS NOT NULL;"
}

# failed_count is work nodes the engine itself marked failed. A run can exit 0
# with failed nodes inside it — bench/run.sh learned this the expensive way.
failed_count() {
  num "$1" "SELECT count(*) FROM nodes WHERE status = 'failed';"
}

# scale_route is the branch the compiler took: single_leaf, bundle, or planned.
# There is no 'fan_out' — the wide shape is 'planned' with leaves > 1.
scale_route() {
  local value
  value="$(q "$1" "SELECT json_extract(payload,'\$.route') FROM events WHERE kind='scale_gate' ORDER BY seq DESC LIMIT 1;")"
  echo "${value:-none}"
}

# scale_field reads any other field off the newest scale_gate: leaves, parts,
# structure, scale, goal.
scale_field() {
  q "$1" "SELECT json_extract(payload,'\$.$2') FROM events WHERE kind='scale_gate' ORDER BY seq DESC LIMIT 1;"
}

# plan_graph_nodes is how many nodes the planner's document described.
#
# A plan_graph event is emitted on *every* route, including single_leaf, where
# it carries a one-node graph. So "did this run plan?" is not a question about
# the event's existence — it is this count. Asserting the event's absence would
# fail on a correct single-leaf run.
plan_graph_nodes() {
  num "$1" "SELECT json_array_length(payload,'\$.graph.nodes') FROM events WHERE kind='plan_graph' ORDER BY seq DESC LIMIT 1;"
}

# plan_graph_stages is how many distinct stages the plan document used. Two or
# more is a staged plan — diagnose before fix, rather than one undifferentiated
# lunge at the goal.
plan_graph_stages() {
  local value
  value="$(q "$1" "
    SELECT count(DISTINCT json_extract(node.value,'\$.stage'))
      FROM events, json_each(json_extract(events.payload,'\$.graph.nodes')) AS node
     WHERE events.kind='plan_graph'
       AND events.seq = (SELECT max(seq) FROM events WHERE kind='plan_graph');")"
  echo "${value:-0}"
}

# plan_has_questions says whether the run surfaced anything it did not know.
# Open questions are the difference between a plan and a guess.
plan_has_questions() {
  num "$1" "SELECT count(*) FROM events WHERE kind IN ('agent_question_queued','agent_question_surfaced','assumed_with_default');"
}

# growth_rounds counts division/growth events — the engine deciding mid-run that
# the shape it started with was not enough.
growth_rounds() {
  num "$1" "SELECT count(*) FROM events WHERE kind = 'job_growth';"
}

# splices counts subtree_spliced events: how many times work was grafted in.
splices() {
  num "$1" "SELECT count(*) FROM events WHERE kind = 'subtree_spliced';"
}

# ── concurrency ─────────────────────────────────────────────────────────────

# concurrent_starts is the largest number of work nodes that started within
# <window> seconds of the earliest start. This is the parallelism measurement:
# leaves that begin together were scheduled together.
#
# The window is measured from each candidate start, not only from the global
# earliest, so a run that starts six leaves together after one preliminary node
# still reads as six.
concurrent_starts() {
  local db="$1" window="${2:-2}"
  local value
  value="$(q "$db" "
    SELECT COALESCE(max(cohort), 0) FROM (
      SELECT (SELECT count(*) FROM nodes AS peer
               WHERE peer.parent_id IS NOT NULL
                 AND peer.started_at IS NOT NULL
                 AND julianday(peer.started_at) >= julianday(anchor.started_at)
                 AND (julianday(peer.started_at) - julianday(anchor.started_at)) * 86400.0 <= $window
             ) AS cohort
        FROM nodes AS anchor
       WHERE anchor.parent_id IS NOT NULL AND anchor.started_at IS NOT NULL
    );")"
  echo "${value:-0}"
}

# start_spread_seconds is the wall time between the first and last work node
# starting. Recorded for the record rather than asserted: it reads very
# differently for a fan-out than for a chain, and both are legitimate shapes.
start_spread_seconds() {
  local value
  value="$(q "$1" "
    SELECT ROUND((julianday(max(started_at)) - julianday(min(started_at))) * 86400.0, 2)
      FROM nodes WHERE parent_id IS NOT NULL AND started_at IS NOT NULL;")"
  echo "${value:-0}"
}

# ── dependency structure ────────────────────────────────────────────────────

# max_fan_in is the largest number of hard dependency edges pointing at any one
# node — the width of the widest gate in the graph.
#
# This is the dependency-pot sentinel. When the pot was capped at 4 KiB, parts
# were dropped silently: the sink still ran, still produced a plausible table,
# and the only evidence was that it was gated on fewer edges than there were
# parts. A run that answers six questions from four inputs is not a correct run.
max_fan_in() {
  local value
  value="$(q "$1" "
    SELECT COALESCE(max(inbound), 0) FROM (
      SELECT count(*) AS inbound FROM edges
       WHERE kind IN ('feeds_into','blocks')
       GROUP BY to_id
    );")"
  echo "${value:-0}"
}

# edge_count is every hard dependency edge in the graph.
edge_count() {
  num "$1" "SELECT count(*) FROM edges WHERE kind IN ('feeds_into','blocks');"
}

# sink_id names the node the widest gate feeds — the synthesis node, when there
# is one. Empty when the graph has no edges at all.
sink_id() {
  q "$1" "
    SELECT to_id FROM edges
     WHERE kind IN ('feeds_into','blocks')
     GROUP BY to_id ORDER BY count(*) DESC, to_id LIMIT 1;"
}

# ── money and verdicts ──────────────────────────────────────────────────────

# total_cost is every dollar the run's own usage accounting recorded, work and
# overhead together. Same source as the "$<spend>" figure `do` prints, and the
# same discipline bench/README.md sets out: self-reported usage, never an
# account-level credit delta on a shared key.
total_cost() {
  local value
  value="$(q "$1" "SELECT ROUND(COALESCE(SUM(cost),0), 6) FROM usage;")"
  echo "${value:-0}"
}

# total_tokens is prompt plus completion across the run.
total_tokens() {
  num "$1" "SELECT COALESCE(SUM(prompt_tokens + completion_tokens),0) FROM usage;"
}

# models_used is every distinct model slug that was actually billed, joined with
# '+'. This is how a cell proves it ran on the model it claims: the pin is not
# what was asked for on the command line, it is what the journal says was paid
# for. A row whose model column surprises you is a row to throw away.
models_used() {
  local value
  value="$(q "$1" "SELECT DISTINCT model FROM usage WHERE model != '' ORDER BY model;" | paste -sd+ -)"
  echo "${value:-unknown}"
}

# delivery_pass is the engine's own delivery-gate verdict: 1, 0, or empty when
# no gate ran. Recorded beside the battery's independent checks, never in place
# of them.
delivery_pass() {
  local value
  value="$(q "$1" "SELECT json_extract(payload,'\$.pass') FROM events WHERE kind='delivery_gate' ORDER BY seq DESC LIMIT 1;")"
  echo "${value:-na}"
}

# delivery_gap is what the gate said was missing, when it refused.
delivery_gap() {
  q "$1" "SELECT COALESCE(json_extract(payload,'\$.gap'),'') FROM events WHERE kind='delivery_gate' ORDER BY seq DESC LIMIT 1;"
}
