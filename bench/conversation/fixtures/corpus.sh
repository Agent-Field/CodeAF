#!/usr/bin/env bash
# corpus.sh — the research and writing workloads' fixture: a small document set
# with facts planted in it.
#
# Research here means "find and join what is written down", not "recall what the
# model was trained on". That choice is deliberate and it is what makes the cell
# checkable: the facts are invented tokens that exist nowhere else, so an answer
# containing them was read out of the corpus rather than remembered, and an
# answer missing them is missing regardless of how well it reads.
#
# The three answers are each in a different file, and one of them contradicts an
# older note in a fourth file — a corpus with no contradiction in it rewards
# whoever reads the fewest files.

fixture_corpus() {
  local work="$1"
  mkdir -p "$work/notes"

  cat > "$work/notes/deploy.md" <<'MD'
# Deploy notes — Kestrel service

The Kestrel service is deployed from the `release/kestrel` branch.
Its health endpoint is `/healthz` and it listens on port 8431.

Rollbacks are performed with `kestrelctl rollback --to <tag>`; the tags are
dated, not numbered. The last three rollbacks all took under four minutes.

The on-call rotation for Kestrel is owned by the team codenamed BRACKISH.
MD

  cat > "$work/notes/incidents.md" <<'MD'
# Incident log (excerpt)

## INC-4471 — Kestrel latency, 2026-02-11
Cause: the retry budget was shared between the read and write paths, so a slow
write starved reads. Fixed by splitting the budget. Peak p99 was 2310 ms.

## INC-4488 — gasket cache stampede, 2026-02-27
Cause: cache entries expired on a fixed wall-clock minute, so every instance
refetched together. Fixed with jitter. No customer impact.

## INC-4502 — Kestrel deploy blocked, 2026-03-14
Cause: the release branch had drifted from `main` by 43 commits.
MD

  cat > "$work/notes/ownership.md" <<'MD'
# Ownership

| service | team      | pager     |
| ------- | --------- | --------- |
| kestrel | BRACKISH  | pd-brack  |
| gasket  | TINDERBOX | pd-tinder |
| flange  | BRACKISH  | pd-brack  |

BRACKISH owns two services and TINDERBOX owns one. Escalation for either goes
to the duty lead, never directly to a service owner.
MD

  # The stale note. It is older than ownership.md and says the opposite; an
  # answer that quotes it without noticing has read one file and stopped.
  cat > "$work/notes/old-ownership.md" <<'MD'
# Ownership (SUPERSEDED — kept for history, see ownership.md)

Last updated 2025-11-02. Kestrel was owned by TINDERBOX at that time.
MD
}
