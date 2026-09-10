---
kind: removed
title: the v1 window and internal/command — 111 lines moved, 36,037 deleted
pr: 792
surface: [chat, resident, build]
invalidates:
  - "`internal/tui` was \"v1, and the visual north star: restrained, dim telemetry, no borders\" — CLAUDE.md's surface table said so and `internal/iconlaw` swept it. **There is no v1 surface in this tree.** The north star was never a property of a Go package: `docs/DESIGN-LANGUAGE.md` is the north star, and that sentence now lives beside it in CLAUDE.md."
  - "`internal/command` existed and was \"untouched\" (entry 469), described as \"the surface's hand on the machine\" — every capability a window could reach the machine through. It is gone. Four helpers moved into `cmd/aforge/chatprefs.go`: `Prefs`, `MediaModels`/`NewMediaModels`/`Snapshot`, `attachmentStoreRoot` and `loadChatPrefs`/`newSessionID`. Every `Commander` method — `Settings`, `CatalogFor`, `SetModel`, `Budget`, `Standing`, `Cancel`, `Restart`, `ConfirmSurgery`, `Notebook`, `SearchNotebook`, `RetractNotebook`, `NewSession` — was reached only by the window's key presses; v3 has its own door for each."
  - "Entry 469 said the alias spellings `loadChatPrefs`, `attachmentStoreRoot` and `newSessionID` \"stay, because an errand still uses them\" and that `internal/command` itself was untouched. The names stay and the errand still uses them, but they are no longer aliases onto another package — they are declared in `cmd/aforge/chatprefs.go`."
  - "**The worker trace has no reader.** `Commander.NodeTraceSince` and `NodeTraceTail` were the only production callers of `exec.TracePath` in the module, and `internal/tui3` and `internal/session` contain no reference to it at all — v3 never read a worker's `.aforge/trace/` file this way. `internal/exec/executor.go` still WRITES those files. `exec.TracePath` survives with its own test and nothing in production calling it. Either v3 grows a reader or the trace is write-only telemetry, and its writer earns its own audit."
  - "`github.com/charmbracelet/bubbles` was a direct requirement `replace`d onto `./internal/tui/charmbubbles`, a vendored fork inside the v1 window. Both the `replace` and the `require` are gone, and `go mod tidy` also removed the direct `charm.land/lipgloss/v2` requirement — the v1 window was its only consumer — and demoted `colorprofile` and `termenv` to indirect."
  - "`internal/iconlaw`'s `surfaces` comment ended \"A package joins this list the day its sweep lands, and never leaves it.\" One left. The sentence now says the distinction that matters: a package leaves when the package stops existing, which is not the same as a live surface quietly dropping out of the sweep. The law walks three packages, and the failure message no longer offers `Model.icon in internal/tui` as a glyph door — `palette.glyph` / `app.icon` in `internal/tui3` is the door."
  - "`internal/guard`'s `lockedPackages` and `internal/lane`'s forbidden-import list both named `internal/tui`. Neither does. `lockScanFloor` stays at 60 and the guard still reports the same 40 loose lock sites, so `.github/known-red.txt` and `internal/ci`'s `knownRedEntries = 1` are unchanged."
  - "`docs/FEATURES.md` said \"completeness tests in `internal/head` and `internal/tui` fail the build\" when a slash command, model slot, key chord, belt tool or command kind lands without a manual page. Neither of those is where the gates live: the retrieval probes are in `internal/manual`, the slash-command and alias gate is `internal/tui3/manual_test.go`, and the tool-belt gate is `internal/session/manual_test.go`."
  - "`docs/CHAT-V3.md`'s two V3-3 cutover rows said the cutover would \"delete `internal/tui`, `internal/tui2`, `internal/head`, the chatv2 gate\". Two of the four are done. `internal/head`'s conversational cohort and the chatv2 gate are still there, and the rows say so rather than describing a finished plan."
  - "Fourteen `cmd/aforge` tests went with `*command.Commander`, and six behaviours they covered have **no current-surface test**: voice spend on the durable ledger, boost and plan follow-work/override/clear preferences, media preferences reaching a future leaf snapshot, the `/budget default 35` and `/budget unlimited today` forms, and the durable restart command's journal row. The code that implemented them is gone, so these are not regressions — but anybody who believed those behaviours were pinned by a test should stop believing it. `chat_test.go`'s other 28 tests, which are live `aforge do` behaviour, are kept."
  - "`internal/voice` now has no importer at all — `internal/command` was its last one. `internal/thread`, `internal/home`, `internal/head`, `internal/catalog` and `internal/provider/pool` all lost that importer too but keep others. Nothing was deleted on that basis; it is a follow-up with its own audit."
---

The whole change turns on **two thin edges**, each one symbol wide, and on one
correction to how a dead-code pass reads Go: **the blank identifier is not a
caller.** `var _ tui.Commander = (*Commander)(nil)` is a promise about a shape,
and read as a declaration it keeps every type it names looking reachable — which
is exactly the costume a package wears while nothing runs it. That is why 36,037
lines looked alive behind 111 lines of ordinary helpers.

Four symbols inside the moved line ranges were deliberately left behind to die
with the package — `fallbackModels`, `MediaModels.Set`, `SavePrefs` and
`scratchRootFor`, each with no caller outside `internal/command`. Moving them
would have moved dead code into a live package, which is the opposite of the
law that a capability with nothing behind it is absent rather than broken.
