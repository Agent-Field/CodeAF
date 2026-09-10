---
kind: removed
title: the v2 remnants — the root compositor, the model picker, the blocks engine
pr: 790
surface: [chat]
invalidates:
  - "`internal/tui2` was \"a shared component library\" of six packages — entry 111 named `tokens`, `blocks`, `prose`, `modelui`, `reltime` and the root compositor, and CLAUDE.md's surface table listed five of them. It is three packages and a fragment: `tokens`, `prose`, `reltime`, and what is left of `modelui`. `internal/tui2` itself is no longer a Go package at all: the shell, the compositor, the layout solver, the pane geometry, the placeholder surface, the terminal capability probe and the OSC notification writer are gone."
  - "`internal/tui2/blocks` was the block engine the surface drew with, and `internal/tui2/tokens` was documented as plugging into it — `tokens.NewStyler` \"implements blocks.Styler\", with the import edge running tokens → blocks to keep blocks a leaf. Nothing drew with it. Its only non-test importer was `tokens/styler.go`, for two `var _` assertions and the four methods written to satisfy them, and no caller outside `internal/tui2` ever named one. `Styler.Paint`, `Styler.PaintGlyph`, `Styler.PaintIdentity` and `Styler.Token(state, hue)` are gone with the package; `PaintToken`, `PaintOn`, `PaintRowOn`, `Fg` and `Glyph` are what v3 paints through."
  - "`internal/tui2/modelui` was \"the model surface as a component: the chip that says what a thing runs on, and the picker that changes it\", and three surfaces were documented as embedding the chip. There is no chip and no picker. `ModelWord` and the model words are all that is left of the package, and `internal/tui3/spendplace.go` is their only caller."
  - "`internal/tui2/tokens` held the surface's layout policy in `breakpoints.go` — `RailAtWidth` and the rail widths, `DialogFullscreenBelowWidth`, `ComposerMaxRows`, `SplitDiffAtWidth`, `HUDRowCap`, `ContextWarnPoint`/`ContextToken`, and the `FooterColumnOrder` registry with `FitFooter`. All of it is gone, and v3 carries its own widths. `ContextToken`'s only external caller had been `modelui/chip.go`; the footer registry never had one."
  - "`tokens.IdentityFor`, `tokens.IdentityNext` and `tokens.AssignIdentities` were 5.16's identity assignment — a stable pastel per task, no two adjacent rail cards sharing. They had no production caller anywhere in the module and are gone. The eight-pastel band itself stays: `Identity`, `Identity0`, `IdentityCount`, `BandFor`, `IdentityIndex` and `HueIdentity` are untouched, so what went is the helper that chose one, not the vocabulary."
  - "`tokens`' `literalExemptions` held two files allowed to spell a vocabulary byte — `../modelui/result.go` and `../golden/diff.go`. Both are stale: `result.go` is deleted here and `internal/tui2/golden` has not existed since 2026-08-31. The map is empty now, and an exemption still needs a written reason. `scannedPackages` keeps `../modelui`, because `word.go` keeps that directory alive."
  - "`internal/lane`'s `TestNothingHereOpensAConnectionOrDrawsAnything` forbade importing `internal/tui2`. That path no longer exists, so the entry is gone; `internal/tui`, `internal/tui3` and `internal/session` are still on the list."
  - "The manual's what-i-remember page illustrated a memory shelf with the sample row \"tui2 is the live tree; tui is dead code · quirk\". The chat reads these pages to answer questions about itself, so that row is replaced — and it was already false before this change."
  - "The plan for this wave had `internal/head/absorb.go` down as dead. It is not: `internal/head/head.go` calls `deliveredRow` and `absorbDeliveryBounded`, both declared there. `internal/head` is untouched, and its conversational `Head` cohort remains a follow-up that needs a `go/types` dead set — and needs `internal/registry`'s law moved off parsing `internal/head/toolbelt.go` by path."
---

Reachability here is a file-and-symbol question, not a package one: every package
under `internal/tui2` is package-reachable from `cmd/aforge`, so `go list -deps`
says nothing useful about any of it. What decided each row was who names which
declaration — and the answer was four thin edges, each one symbol wide.

Two laws that used `blocks` to build a sample but were about `tokens` are
re-pointed rather than deleted: the tier width-parity golden now composes its
rows from `Glyph` and `PaintToken` (regenerated, still recording every
codepoint), and `TestUpgradeChromeIsAnExplicitDoor` uses token literals.
`TestPaintNeverChangesWidth` survives as `TestPaintOnNeverChangesWidth`. What
genuinely stopped being checked is listed in the pull request.
