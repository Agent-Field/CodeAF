# Home rethink — decisions and lanes

Branch `home/rethink-v0` (worktree `~/af-home`), cut from `origin/chat-v3-task`; merges back into
`chat-v3-task` when the waves land. Read in this order: `SCREENS.txt` (the design), `RECON.md`
(the home we are replacing), `DATA-AUDIT.md` (what data exists), `PAGES.md` (the pages and the
router). This file is the plan those four feed.

> **OVERRULED 2026-08-25 evening:** the owner ordered "follow the exact design". Decision 3's
> softened palette and decision 5's two-word rope are superseded by `FIDELITY.md`, which is now
> the authority on every visual and behavioural detail. `ARCHITECTURE.md` governs structure.

## Decisions (settled 2026-08-25; the owner may overrule any of them)

1. **Target surface is `internal/tui3`.** `github.md`'s tui2 paths are only where the palette and
   glyph slots live. Nothing in `internal/tui2` is touched.
2. **Router: a `page` enum and one shared `placeFrame`** (PAGES.md §4). The existing structs stay as
   page bodies behind adapters; `standDownFullscreen` becomes the private half of `showPage`.
   Go uses the word `page`; people read the word *place*. One key grammar for every place.
3. **Palette: same meanings, tui3's own lightness.** The mockups' hexes fail the pinned laws
   (`#E6E6F0` glares at 13.8:1; amber/cyan/green break the isoluminant band; tui3 paints no ground).
   So: ground, band, body, dim and failure inks stay as they are. "Needs a human" settles on amber
   (`hueWarn`) on home and its places — home says it in two colours today. One new ink `hueMoney`
   at tokens' mint hue (H144) at the table's lightness, for money. The chat surface does not change
   in wave 0; moving the chat's `hueAsk` onto amber is an owner decision, noted in `screen.md`.
4. **Keys — the six-class law from screen 3a**, with two adaptations for terminal reality:
   `alt+1…7` jump to a place (the manual gate that bans the literal `alt+1` is narrowed in the same
   commit, with the reason in the test); `tab` is next place; `alt+letter` changes how THIS place is
   shown (`alt+g` group by project, `alt+q` hide the quiet ones, `alt+t` only what was tidied);
   `shift+←→` moves a place's time window, `shift+↑↓` its grain; `→` opens the verb strip and only
   while it is drawn are bare letters verbs; `esc`/`←` close it. A terminal cannot see a held
   modifier, so the "hold alt" map of 3b is **`alt+.`**: it draws the map in the same cells until
   the next key. Digits keep answering a drawn question (the existing `answers.go` path); the
   verb strip may offer letters for the same answers.
5. **Vocabulary.** No resident words. `tenured` / `earning tenure 3/5` do not exist here — there is
   no probation counter — so the rope column is exactly two words: **on its own** (the item has a grant —
   `Item.Grant` is what it may do *without* asking) or **asks first** (no grant; it may only tell
   you things). See `standing.RopeWord`'s comment; `NeedsPerson` is a state, not rope. `alt+1` is allowed only as the chat's place
   key. No machinery words on screen.
6. **Data we add (cheap, additive), all in `internal/session` / `internal/store`:**
   - `TaskIndexEntry.Kind` — from `taskSpec.kind()`, so a row can say *adaptive*, *saved shape*, *job*.
   - A machine-wide **usage ledger** `~/.aforge/v3/usage.jsonl`: one line per model call with day,
     model, role, calls, tokens, cost, session id, task id, standing id — written where
     `journalUsage` is written today, read through a cached day/model/role aggregation.
   - **Per-place look stamps** beside `session.LastLook` — one stamp per place, written when a place
     is left, so a tab can wear a count only when something in it changed.
   - A **standing "last look" line** composed from `LastFired`, `LastOutcome`, `LastCheckLine` — the
     inbox stays quiet by contract.
   - A **memory snapshot reader** for v3's real memory (`store.Memory`: 3 scopes, 5 types, 3 statuses,
     `UseCount`/`MissCount`) that a place reads on home's 3-second clock, never per keystroke.
   - **Search** over the FTS5 message index (`store.SearchMessages`) run as an async command with
     a debounce — never on the draw path.
7. **Not built, because the mechanism does not exist:** model fall-through events ("execution moved
   to sonnet"), self-demoting connections ("gmail moved itself to ask-first"), probation counters,
   the 82→9 consolidation receipt with undo (wave 2 candidate: have consolidation write a receipt).
   Under the emptiness law these lines are simply absent.
8. **Home's own list** follows 1a/1d: one flat ranked list (needs-you, then moving, then quiet),
   project as a tag on the row, the right-hand note carrying the one fact the card was for; a card
   column only at ≥160 columns with five bands; `alt+g` restores grouping. The "since you left"
   ledger draws only when something happened on its own and each line is a door to its place.

## Wave 0 — foundation (two lanes, disjoint packages, parallel)

**L0-records** — worktree `~/af-home-records`, branch `home/records`. Packages: `internal/session`,
`internal/store`, `internal/config`, `internal/standing`. Decision 6 in full, with tests, and the
`go doc`-level API written so a page can call it without reading the implementation. Must not
touch `internal/tui3`.

**L0-router** — worktree `~/af-home-router`, branch `home/router`. Package `internal/tui3` and
`internal/manual/chat`. The `page` enum, `showPage`, `placeFrame` (pulse, tab bar, rule, body, foot
composer with scope chip, hint line), the key classes of decision 4 as one dispatcher, the verb
strip mechanism (lifted from `answerstrip.go`), the `alt+.` map, `hueMoney` and the amber move on
home, the per-place adapters so that `home`, `tasks`, `standing`, `memory`, `settings` open as
places today with their existing bodies, and empty `spend` / `search` places that teach what they
are for. Manual pages updated in the same change; all three gates green; `make check` green except
the known-failing list in CLAUDE.md. Must not touch `internal/session`.

Both lanes: commit early and often, push the branch, and finish by merging into `home/rethink-v0`
through a detached worktree if the tree is busy. Never `git add -A`.

## Wave 1 — the places (after wave 0 merges; parallel worktrees)

- **L1-home** — the flat list, the ledger, the ≥160-col card with five bands, `alt+g`, 1b's
  answer-in-place strip.
- **L1-tasks** — 1e: grouped by state, cost column, header sentence, fold, time window, *adaptive* word.
- **L1-standing** — 2f: promoted to a page, rope column, cadence, cost per firing, last-look line.
- **L1-memory** — 1f/2d on `store.Memory`: shelves by scope, kind counts on the section line,
  helped/bore-on words, let-go lines struck, the teaching prose when nearly empty.
- **L1-spend** — 2c/3d on the usage ledger: sparkline, window/grain keys, by model and role, what it
  was for; no budget editor.
- **L1-search** — the FTS place with facets (place, project, kind), typed results offering places
  (1g).
- **L1-composer** — 2e: the composer as a layer on any place with the three task facts, `alt+enter`.

Each lane owns its manual pages and its tests; the manual law applies to every lane.
