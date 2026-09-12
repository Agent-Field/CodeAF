---
kind: removed
title: the planning dumps, the superseded plans and two orphan packages — 344 files, 2,933 Go lines
pr: 991
surface: [docs, build, chat, resident]
invalidates:
  - "`ideation/`, `issues/` and `perf-report/` were folders at the repository root, and `test/ux/` was the executable journey suite beside `test/remote/`. **None of the four exists.** The one thing in `ideation/` that live code cites is now `docs/design/routing/provider-routing.md` — `internal/lane`'s comments and tests spell that path — and `harness-research-notes-set2.md` is now `docs/research/harness-research-notes-set2.md`. `harness-research-notes.md` is still at the root, because three of its citers are in files an open pull request owns."
  - "`test/ux/run.sh` and `gate.sh` were how a journey got driven. They launched `aforge chat` v1 and `aforge chat --v2`, and neither surface has existed since 2026-08-31, so the scripts could not have run. `internal/e2e` is the only suite that drives the real binary, and it covers selected v3 scenarios rather than one script per row of `docs/JOURNEY.md` — the page says so now instead of promising an index."
  - "Entry 792 said `internal/voice` \"now has no importer at all\" and that \"nothing was deleted on that basis; it is a follow-up with its own audit.\" **This is that follow-up: `internal/voice` and `internal/gate` are gone**, 1,039 lines of them plus 911 lines of their tests. `docs/MULTIMODAL.md` said the voice machinery \"is real and waits on a surface wave\" — a wave that wants a microphone now builds the capture afresh against `internal/provider`'s `Transcribe`, which is the only transcription transport in the tree and whose wire shape `internal/provider/transcribe_test.go` is now the only thing pinning."
  - "`docs/CHAT-V3.md`'s module-shape paragraph named `internal/gate` as \"the one seam\". There is no such package; the sentence is in the past tense and names no path."
  - "`internal/guard/sweep_test.go`'s `guardedPackages` and `spawnAllowlist`, and `internal/provider/funnel_law_test.go`'s `funnelKnownSecondTransports`, each carried an entry for a file in `internal/voice`. All three entries are gone, which makes those laws **stricter**, not weaker: there is one fewer exempt goroutine site and one fewer sanctioned second transport to a model endpoint."
  - "Sixty-eight functions `deadcode -test ./...` reported unreachable from every `main` and every test are deleted, across 21 packages. The exported ones somebody may remember as API: `config.ModelCostHint`, `exec.RegisterManifest`/`ManifestFor`/`ForgetSubharnesses`/`StreamFile`, `lane.AllowLowQuantization`/`Patience`, `plan.AcceptanceQuotes`/`Audit`, `provider.CallHorizonFrom`/`OpenOffer`/`PathFault`, `registry.All`/`Len`, `relay.Refuse`, `remote.MemoryOff`, `resident.CraftCommit`, `revision.ForUser`, `store.FormatCompetence`/`CredibilityWord`, `subharness.OpNames`, `modelui.RoleWord`, and `tokens.GroundResets`/`Underline`/`UnderlineOff`/`UnderlineColorOff`. `internal/tui3` and `internal/session` keep theirs: twenty-seven open pull requests live in those two packages."
  - "`cmd/harness-design`'s `driver.Plan` and `driver.Exec` read as dead and are not — `orchestrate.go:53-54` assert `_ orchestrate.Planner = (*driver)(nil)` and the same for `Executor`. `internal/registry` reads as an orphan and is not: its test is a law that parses `internal/head/toolbelt.go` by literal path, which also fixes that file in place. Neither was removed."
  - "`docs/design/polish/frames/` and the raw ledgers under `bench/ab-routing`, `bench/probelab` and `bench/routerlab` look like dead weight and **stay**: `scripts/gallery.py` reads the frames, about fifteen kept `docs/design/polish/*.md` audits link to them by name, and `analyze.py`, `replicate.py`, `merge_results.py` and `summarize.py` read those exact jsonl files. The frames no longer carry a developer's home directory — the substitution was length-preserving, so every captured column and every OSC-8 hyperlink payload is byte-identical."
  - "`bench/oneroad/marathon/{cell,finish,rescore}.sh` defaulted `MARATHON_REPO`, and `bench/oneroad/swe/build.sh` defaulted `LOGS`, to a private scratchpad path under `/tmp` that existed on one machine. They default under `$HOME/af-bench/` now, so a caller who exported nothing gets a location that at least belongs to them."
  - "`docs/coordination-design.md`, `docs/home-design.md`, `docs/host-parity.md`, `docs/host-parity-commands.md`, `docs/remote-files-plan.md`, `docs/small-models-sota-2026.md`, `docs/sub-10b-sota-2026.md`, `docs/design/folder-experience/WAVE.md` and `docs/design/test-speed/PLAN.md` were documents in the tree. They are deleted; `docs/HOME-BRIDGE.md` carries the home layout's three jobs and `docs/ARCHITECTURE.md` the coordination argument. `docs/conversations-design.md` and `docs/remote-access-plan.md` stay — deleting either needs an edit to a file this change may not touch."
  - "`audit-notes/` held seventeen files. It holds two: `design-law-v2.md`, which `internal/session/session.go` cites, and `headless-regression-audit.md`, which a changelog entry names. Comments that pointed at `chat-rebuild.md`, `chat-simplify.md`, `rail-rooms-grooming.md` and `world-grounded-planning.md` now name the August 2026 audit and its section without a path, because the file is not in the tree to open."
  - "`docs/design/home-rethink/` held the Claude Design export — `Home Rethink.dc.html`, `support.js`, `uploads/`, the thumbnail, `SCREENS.txt`, `RECON.md`, `PAGES.md`, `github.md`, `REVIEW-final.md`, `QA-INTERACTION.md`. All ten are gone and `FIDELITY.md` is the retained authority for the screens' rules; `internal/tui3/pulse.go` and `spendplace.go` cite it rather than the export. `docs/design/pin-doe/` lost `cells/` (191 files), `golden/` and `sealed.json`; `report.md` and `report-table.md` carry the findings."
  - "`internal/.DS_Store` was tracked. It is not, and `.gitignore` refuses the next one."
---

Three path references are left dangling **on purpose**, because each is a record
of a moment rather than a pointer to a file: `docs/changes/unreleased/199-load-and-midnight-flakes.md`
quotes the old `ideation/` spelling inside a quotation,
`docs/changes/unreleased/740-the-test-tooling-tells-the-truth.md` names the
removed `docs/design/test-speed/PLAN.md` as one of three places a stale claim
lived, and `docs/design/gate/muse-evidence.json` embeds a verbatim tree listing
that `internal/revision/grounding_test.go` reads as a fixture. Rewriting any of
the three would falsify what was recorded.

Nothing test-only was removed. The cohorts that looked removable each still carry
live non-test declarations — `internal/head/head.go:353` calls into `absorb.go`,
`internal/tui3/folderplace.go:1235` calls into `folderindex.go`,
`internal/store/message_parts.go` holds JSON hooks four packages import, and
`internal/tui2/tokens/profile.go` uses `format.go`'s `appendInt`. A file whose
every *function* is test-only is not a file whose every *declaration* is, and the
previous wave already learned that the expensive way on `absorb.go`.

One thing for the owner rather than for this change: `internal/manual/chat/commands.md`,
`internal/provider/attribution.go`, `internal/tui3/detach.go`,
`internal/tui3/slashchip.go`, `docs/design/conversation-runtime/**` and
`docs/design/workspace-foundation/CONSTRAINTS.md` still carry a personal path, a
personal name, or a reference to the removed voice package. Every one of them is
owned by an open pull request, so they were left exactly as they were.
