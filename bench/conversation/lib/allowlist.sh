#!/usr/bin/env bash
# allowlist.sh — the open-model policy: which models this suite may cause calls
# to, and how that is checked.
#
# Three gates, because no single one is sufficient:
#
#   environment  a harness inherits OPENROUTER_API_KEY and no other credential
#                (common.sh: CONV_CARRIED), so it cannot reach a provider this
#                run did not intend.
#   before       the arm is handed the exact pinned id, its catalog is asked
#                whether that id exists, and an arm that cannot pin its
#                auxiliary roles to the same model is skipped rather than run
#                (adapters.sh: arm_pin_check, arm_role_pin).
#   after        every model id in the cell's own receipts is checked against
#                the list. This catches what a flag cannot: a role, a title
#                call or a fallback that resolved elsewhere.
#
# Entries are exact catalog ids. No wildcards and no family names: matching
# `deepseek-v4-flash` as a substring would accept its dated siblings too.

# CONV_MODEL is the pin every arm is handed, in that arm's spelling
# (adapters.sh: arm_model_arg). The default is DeepSeek V4 Flash at a dated id;
# a floating alias is free to move under a benchmark.
CONV_MODEL="${CONV_MODEL:-deepseek/deepseek-v4-flash-0731}"

# CONV_ALLOWLIST is every id this run may legitimately have billed. When it is
# not set explicitly it follows CONV_MODEL, including after --model moves the
# pin: an allowlist left pointing at the previous default would permit exactly
# the model the caller just chose to stop using.
CONV_ALLOWLIST_EXPLICIT="${CONV_ALLOWLIST:+yes}"
CONV_ALLOWLIST="${CONV_ALLOWLIST:-$CONV_MODEL}"

# allowlist_follow_model is called after argument parsing, so an explicit
# allowlist wins and an implicit one tracks the chosen model.
allowlist_follow_model() {
  [ -n "$CONV_ALLOWLIST_EXPLICIT" ] || CONV_ALLOWLIST="$CONV_MODEL"
}

# normalise_model drops only the spellings that are the same configuration under
# a different prefix: aforge's `~` alias marker and the `openrouter/` prefix omp
# and opencode want. A `:batch` or other variant suffix is KEPT — batch routing
# is a different queue with different latency and price, so it is a different
# entry and has to be allowlisted on purpose.
normalise_model() {
  local model="${1-}"
  model="${model#\~}"
  model="${model#openrouter/}"
  printf '%s' "$model"
}

# model_allowed answers for one id. An empty id is not allowed: "the receipts
# did not say which model was billed" is a finding of its own and must not be
# waved through as an allowed model.
model_allowed() {
  local got entry
  got="$(normalise_model "${1-}")"
  [ -n "$got" ] || return 1
  for entry in $CONV_ALLOWLIST; do
    [ "$got" = "$(normalise_model "$entry")" ] && return 0
  done
  return 1
}

# models_outside_allowlist prints, one per line, every id in its arguments that
# the law does not permit. The caller decides what to do with them; nothing here
# is silent about them.
models_outside_allowlist() {
  local model
  for model in "$@"; do
    model_allowed "$model" || printf '%s\n' "$model"
  done
}

# allowlist_banner is printed at the head of every run, so that the file of
# evidence a run leaves behind says on its first page which models it was
# allowed to reach.
allowlist_banner() {
  printf 'model pin:  %s\n' "$CONV_MODEL"
  printf 'allowlist:  %s\n' "$CONV_ALLOWLIST"
  printf 'policy:     open models only — a receipt naming anything else fails its cell\n'
}
