# Wave 1 progress — verified candidate

Exported by `t-w1-ready` (`cw0918-ready`) from PlanDB. This is a graph/status
snapshot, not a copy of `implementation.db`.

**Verified SHA:** `4b3b407a676efca3282b9834f05ea956c98a89cd`  
**Branch:** `feat/collaborative-workspace-0918` (pushed; no merge to `dev`)  
**Exported:** 2026-09-19T03:05:00Z  
**JSON dump (control, not this repo):** `/home/santosh/src/codeaf-workspace-0918-control/plandb-wave1-dump.json`

## Owner launch (copy-paste)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
```

Immutable binary: `/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/codeaf`  
sha256: `d84c5824bcfbf43fc51d154384123f516b2642525e490afe24f1995294277ff9`  
Isolated `CODEAF_HOME` + `CODEAF_PROFILE_DIR`. Never `HOME`. Never `~/.codeaf`. Do not install globally.

## Gate

| Check | Status | SHA | Evidence |
|---|---|---|---|
| storage review | pass (`ok: true`; no `workspace`/`wsapi` diff after 4519d02f) | `4b3b407a676efca3282b9834f05ea956c98a89cd` | `/home/santosh/src/codeaf-workspace-0918-control/workers/review-storage/review.md` and `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-1/review-gate.md` |
| journeys review | pass on this SHA after remediations (original `ok: false` on 4519d02f) | `4b3b407a676efca3282b9834f05ea956c98a89cd` | `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-1/review-gate.md` |
| affected | pass (`t-w1-affected3`; first two runs failed on leaked `PLANDB_DB`) | `4b3b407a676efca3282b9834f05ea956c98a89cd` | `/home/santosh/src/codeaf-workspace-0918-control/workers/affected3/test.log` |
| live TUI J01–J08 | pass (79 assertions, 69 live / 10 synthetic, 0 skip, 0 fail) | `4b3b407a676efca3282b9834f05ea956c98a89cd` | `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-1/journey.json` |

Receipt: `/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/ready.json`  
Issue result: `/home/santosh/src/codeaf-workspace-0918-control/issue-1-result.json`

## PlanDB place graph (Wave 1)

Parent `t-wave1` Wave 1: folders and shared chats.

- done: contracts, storage, service, tui, wiring, proof, real-store, integrate, review-storage, review-journeys, tui-j, wire-j, needles, reintegrate, affected3, live
- failed: `t-w1-affected`, `t-w1-affected2` (worker `PLANDB_DB` leaked into session PlanDB CLI tests; those three tests pass with `env -u PLANDB_DB`)
- remediations done: `t-npv6`, `t-2hh1`, `t-de9k`, `t-8ngf`, `t-ghnk`, `t-lspz`
- running at export: `t-w1-ready`
- pending later waves: `t-wave2`, `t-wave3`, `t-wave4`

## What was true, and what is true now

- Independent journeys review on `4519d02f` was `ok: false` (TUI could not nest; opening a chat dropped the folder path; gone members looked like ordinary chats). That is still the stored result of `t-w1-review-journeys`.
- `t-w1-tui-j`, `t-w1-wire-j`, and `t-w1-needles` landed those fixes; `t-w1-reintegrate` produced this SHA; `t-w1-affected3` and `t-w1-live` passed on it.
- Storage review `ok: true` still holds: no `internal/workspace` or `internal/wsapi` change since that review.

No GitHub issues, comments, or PR. Wave 2 is not claimed by this worker.
