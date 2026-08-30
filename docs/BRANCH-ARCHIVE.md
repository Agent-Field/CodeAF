# Branch archive — 2026-08-30

Two branches live on origin: `master` (the release line) and `staging` (the
working line, accumulated from `chat-v3-task` and the measured `chat-v3-fix`
sweep). Everything else was tagged and deleted on 2026-08-30. A tag keeps every
commit forever and stays out of the branch list; bring one back with

    git branch <name> archive/2026-08-30/<slug> && git push origin <name>

`staging` at the time of the archive = `staging-s17` (`2f844bb6`) =
`chat-v3-task` `85c5d624` + the non-UI fixes below + s17 (`2a7c8d77`, the
selfclose / every-observable-door / ofetch-identity sweep, the only DeepSWE run
on the Pareto frontier on all three axes) + four staging laws restated on s17's
seams. Full suite: 189 packages ok; every red test is on CLAUDE.md's known list
or red on both parents.

| tag (`archive/2026-08-30/…`) | was | tip | what it held | what reached staging |
| --- | --- | --- | --- | --- |
| `chat-v3-task` | the live line | 85c5d624 | everything through Aug 29 | all of it — staging's base |
| `chat-v3-fix` | DeepSWE fix wave, `~/af-fix` | a1bd96c3 | s1–s32 sweeps, autopsies, verify/gate lanes | up to s17 (2a7c8d77); the 198 commits past it are unbenched |
| `staging-prep` | last gated-good marker | 80f52f36 | ancestor of chat-v3-task, 0 unique | nothing to port |
| `staging-nonui` | cut of the non-UI fixes | ccd2908b | the six commits below | all |
| `chat-v2-ui` | tui3 chat redesign (misnamed: not v2) | 47a776ab | role sheet, task handle, ctrl+tab switcher, crews, rail retired; 37 design docs | none — design work to be redone |
| `home/rethink-v0` | home redesign, `~/af-home` | 0b59c798 | ask cards, rung renderer, spinparent, page gate; 264 commits | the spin-counter fix, tool-arg decoder, argument-refusal loop rule, "report comes to you" |
| `home/v2-spend` | spend page | a535dbd5 | four bands, places top line | none |
| `home/v2-memory` | memory shelves | 462ea456 | fully absorbed by home/rethink-v0 | via rethink |
| `ui/v0` | theme + failsafe lanes, `~/af-ui` | 0f3be521 | base16 scheme file, refusal ladder, evidence, leaf log | refusal ladder, ceiling memo, compaction panic, gate citations, evidence, leaves (all already on chat-v3-task) |
| `ui/lane-gate` | lane of ui/v0 | fd1acf5e | contained in ui/v0 | via ui/v0 |
| `fix/lane-scope57` | pointer duplicate of chat-v3-fix | e7a83c1d | nothing unique | — |
| `coop/v0` | population / hive experiments | a5ff8597 | bench/coop, bench/hive rigs and REPORT.md | none (bench only) |
| `feat/one-road` | split-swarm-only chat | b5e8c7c2 | marathon budget env; 401 behind | none; the thesis (no in-turn making) stands |
| `cleanup/drop-v1-v2` | delete v1/v2 chat surfaces | c3455e3e | 747 behind | none; redo on staging when wanted |
| `salvage/lane-iii` | progress metering, local-only until today | e0283161 | internal/progress (+796, +1014 test), progress_boundary law test | none yet — unmerged, unbenched |

## The six non-UI fixes ported onto chat-v3-task (staging-nonui)

| commit | fix |
| --- | --- |
| 895140a9 | a part's report buys the no-progress counter one reset, never two — a spinning parent is stopped (was red on chat-v3-task) |
| b6f6ac95 | tool arguments: one decoder, and 10.0 is ten |
| 9fe9fc1a | the decoder law reaches the two hands that arrived after it (workspace anchor, slack) |
| eaef3963 | an identical call refused for its arguments is stopped at two, with the correction |
| 4481ca22 | tasks: the report comes to you — there was never anything to poll |
| ccd2908b | the polling fact is said once; the fixed prefix holds under 48000 bytes |

Not ported, and why: the `esc`-on-a-live-ask segfault and the ask-belt
allow-list target `internal/session/askthread.go`, which exists only with
home/rethink-v0's ask feature. They come back with it.

## The chat-v3-fix merge (staging-s17)

Six conflicts, all seams between staging's Aug 28–29 work and s17's:
`provider/retry.go` union (lane wall + unstreamable guard), `provider/client.go`
union (ledger row + three-value return), `head/compiler.go` s17 (the shaped seam)
with staging's JSON-on-the-wire and blank-assumptions laws restated on it,
`cmd/aforge/chat.go` s17 (`jobArtifacts`), and `exec/bare/tools.go` keeps a
person's interrupt as "Command aborted" beside s17's "cut after" for a deadline.

## Measured on staging-s17 (2f844bb6), happy-dom-deterministic-intersectionobserver, deepseek-v4-flash

See the row in bench/deepswe results for this date. Do arm: f2p 12/14, p2p 9/9,
$0.10, 2174s, exit 2 (the gate refused: five stated behaviours had no check).
