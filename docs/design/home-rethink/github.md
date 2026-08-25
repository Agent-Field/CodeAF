repo: Agent-Field/aforge-v2
branch: master
path: internal/tui2

## Last sync
date: 2026-08-25T18:02:15Z

### Updated in this project
- Home redesign mockups grounded in the real `tokens` palette (#12121A ground, #262633 band, amber/cyan/green states).
- Every state mark re-drawn from existing `tokens/glyph.go` slots; the two reuses are stated on the page.
- Adopted the repo's line/row grammar: gutter glyph, content edge, right-flush note, one ellipsis grammar.
- Added a spend page, a memory page at real scale, standing, and a global composer, all read from store/facts.go, store/usage.go, config/modelslots.go and homes/spend.go.
- Kept the "no budget page" law: the spend page reports, the status segment stays the only rail control.
- Audited every key binding against terminal reality (ctrl+m is enter, ctrl+e is end-of-line in spend.go's field) and re-homed the colliding ones onto a six-class key law.

## Screen map
| Screen | Built from |
| --- | --- |
| Home Rethink.dc.html — 1a/1b/1c/1d home | internal/tui2/homes/view.go, internal/tui2/homes/line.go, internal/tui2/tokens/palette.go, internal/tui2/tokens/glyph.go |
| Home Rethink.dc.html — 1e tasks page | internal/tui2/homes/doc.go, docs/JOBS.md |
| Home Rethink.dc.html — 1f memory page | internal/tui2/homes/home.go (HomeNotebook blurb), docs/LEARNING.md |
| Home Rethink.dc.html — 1g typed places | internal/tui2/homes/route.go |
| Home Rethink.dc.html — 2a design scale | internal/tui2/tokens/palette.go, internal/tui2/tokens/glyph.go, internal/tui2/homes/standing.go |
| Home Rethink.dc.html — 2c spend page | internal/tui2/homes/spend.go, internal/store/usage.go, internal/config/modelslots.go, internal/tui2/modelui/doc.go |
| Home Rethink.dc.html — 2d memory page | internal/store/facts.go, internal/store/recall.go |
| Home Rethink.dc.html — 2f standing page | internal/tui2/homes/standing.go, internal/head/standing.go |
| Home Rethink.dc.html — 3a key law + audit | internal/tui2/homes/spend.go (field bindings), internal/tui2/homes/standing.go (verb registry) |
| Home Rethink.dc.html — 3e consolidation receipt | internal/store/facts.go (FactOriginConsolidator, SupersedeFact) |
