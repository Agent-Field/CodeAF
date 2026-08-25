# Design fidelity — the owner's order: follow the exact design

2026-08-25, the owner: "follow the exact design please." This overrules the softened choices in
LANES.md decision 3 and 5. `SCREENS.txt` (and the .dc.html it was cut from) is the authority;
where two screens disagree, turn 2 (2a–2f) and turn 3 (3a–3e) win over turn 1. Deviations survive
only where the mechanism is physically impossible or the fact does not exist — each such case is
listed at the bottom and stays flagged to the owner.

## The checklist a fidelity lane executes

1. **Palette — the exact tokens hexes on home and every place.** The design quotes
   `internal/tui2/tokens/palette.go`: text tiers `#E6E6F0` / `#A0A6BB` / `#7C8296`; amber `#EECE96`
   = needs a human; cyan `#A4D7EA` = alive; green `#A2E2BC` = money; selection band `#262633`;
   ground `#12121A` as the painted page ground of these surfaces (the chat's own conversation
   surface is untouched). The tui3-local laws in `designlanguage_test.go` (glare ceiling,
   isoluminant band, ground-ladder bands) are ADJUSTED DELIBERATELY for the place surfaces in the
   same commit — the owner signed this; the test comments say so. `hueMoney` becomes exactly
   `#A2E2BC`; needs-a-human on places becomes exactly `#EECE96`.
2. **The pulse, exactly screen 2b/3b:** `aforge` left; right: `2 want you · 4 moving ·
   $0.55 / $20.00 · tue 1:11pm` — the words `want you` and `moving`, the allowance as a fraction,
   each clause absent at zero (1c: a quiet morning is `aforge` and the clock alone).
3. **Hints and feet, word for word:** home at rest `type to search or start something new · ↑↓ pick
   · enter open · tab next place`; with the composer `enter talk about it · alt+enter send it off
   as a task · alt+. for the map · tab next place`; each place's foot as its screen spells it
   (1e, 2d, 2f, 2c). The tab bar words lowercase, counts as bare numbers after the word.
4. **Glyph per state, everywhere:** `?` NeedsHuman (amber) — the switcher must not use `▲`;
   `◐` working (cyan), `○` queued, `✓` settled, `✕` failed, `▸` collapsed/fold, `›` prompt,
   `·` a memory line's bullet.
5. **Memory (1f/2d):** `enter` on a LINE is `ask me about it` — it opens a conversation seeded
   with that line (the card moves behind `→`); `enter` on a shelf opens the shelf. The foot spells
   1f: `enter ask me about it · e fix the wording · f forget it · tab next place`.
6. **Standing rope — three states, screen 2f:** the run history the item already keeps
   (`Runs`, `LastOutcome`, the sentinel lines) derives the middle state: no grant → **asks first**;
   grant present but fewer than 5 clean firings → **earning trust 3/5** (clean = fired with no
   `needsPerson` and no failure); grant present and ≥5 clean → **trusted alone**. Spelling uses
   *trust*, not *tenure* — the resident-separation test bans that word; the mechanism is exact.
7. **Spend roles (2c):** the role column is "the role it was bound to" — join each model against
   the CURRENT crew bindings (`config.ModelSlots`), exactly as the caption says; `planning` with no
   binding draws `unbound · follows execution` (`config.ModelSlotFor`). Not per-call attribution —
   the design itself asks for the binding.
8. **The card at ≥160 (1d), five bands in the design's order and wording:** title; place line
   `~/aforge-v2 · master, 1 file dirty · here`; `it is stopped on you` + the question + its answer
   keys; `work` rows with `✓ … $1.63` and `▸ 3 more tasks` naming the tasks place; `made for you`
   with the path; the facts line `spent $1.63 · 3.6M tokens · thinking high`; then `→ verbs: …`.
9. **Composer 2e, exactly:** the page behind dims to the faintest tier; the three lines are
   `· in ~/aforge-v2, on master` (`alt+w to move it`), `· execution runs on opus 4.1`
   (`alt+o to change`), `· it may spend up to $2.00 before it asks` (`type a number`); the lead line
   `it will run on its own and tell you when it lands` with `a task` right-flushed; foot
   `alt+enter send it off · enter talk about it first · esc back to <place>`.
10. **Screen 3d:** the window label between the arrows is the control and the reading
    (`shift+← aug 12 – aug 25 →` drawn in the header), grain zoom on `shift+↑↓`, the same pair on
    tasks, standing (when it fired) and memory (when it was learned) — memory and standing get
    `placeWindow` too.

## Deviations that remain, and why (each flagged to the owner)

- **"Hold alt" (3b)** — a terminal cannot see a held modifier; the map is `alt+.` (dismissed by the
  next key). The designer's own note offers `esc esc` as the fallback; not built unless asked.
- **"tenure" wording** — the mechanism of 2f is built exactly (three states, counted); the word is
  `trust`, because `manual_test.go` enforces the resident/chat vocabulary wall.
- **Events with no mechanism** — "execution moved to sonnet when opus hit its rail" (no fall-through
  recorder) and "gmail moved itself to ask-first" (nothing self-demotes): the ledger lines exist
  only when the mechanism does; under the emptiness law they are absent, not faked.
- **`project:<dir>` memory shelves (2d)** — `store.Memory` has no workspace column; shelves are
  you / this project / this machine until the store learns one.
