# Standing orders — the ambient side grows a spine

*Design doc for the `standing/v0` line of work. Written before any lane started.
`docs/AMBIENT.md` described the ambient side as it shipped; this document is the
next generation of it. Where this doc and the code disagree, the code wins once
a wave has landed.*

## What this is

Today every agent product, this one included, is **imperative**: work exists
because someone issued a command, and ends when the command completes. The shift
this line of work makes is **declarative**: the person states conditions their
world should satisfy — "the tests never break here", "tell me when an email
actually needs me", "nothing lands that changes the public API" — and the
product continuously keeps those sentences true: noticing drift, spending
bounded money to correct it, and escalating only the decisions that genuinely
need a person.

The person-facing name is a **standing order** — a familiar concept (military,
parliamentary, banking): an order that remains in force until countermanded.
The positioning is deliberately the inverse of the market's "AI chief of
staff": there is no persona above the person. **The person commands; the
product is the standing force that executes.** Internally everything stays in
`internal/standing` — a standing order IS a standing item, grown four fields.

## The laws (in addition to standing.go's own)

- **ONE OBJECT, STILL.** A reminder, a watch, a rule, an invariant, and a
  machine-wide habit are all an `Item`. This wave adds fields to the one
  object; it does not add a second object.
- **YOUR WORDS ARE NEVER REWRITTEN.** `Words` stays the verbatim, permanent
  anchor. The product *compiles* the words into a working `Brief` (a title and
  a prompt), shows the compilation on the ratification card, and the person may
  edit the brief any time. **Editing down is free; editing up is a card** — a
  narrowed brief applies at once, a widened brief (more reach, more action) is
  a new ratification.
- **ONE MIND, MANY HANDS.** Nothing on any surface names a sub-agent, a staff,
  or a manager. When an order acts, the actor is the product's single first
  person, and the act always carries the sentence it served.
- **ALTITUDE IS DECIDED ON THE CARD, NEVER GUESSED SILENTLY.** Where the person
  said it sets the default; scope words in the sentence may promote it; the
  card always names the altitude before anything stands.
- **EXCEPTIONS ARE BORN FROM FIRINGS, NOT FROM BROWSING.** The primary gesture
  for "not true for this project" is one keypress at the moment an order acts
  wrongly there. The page exists for review; the flinch is the workflow.
- **THE ACT LEDGER IS THE TRUST LEDGER.** Every autonomous act lands as one
  news line naming its order. A background that acts without attribution is
  spooky; with attribution it reads like a colleague's standup.
- **ONE POOL, NOT N KNOBS.** Money is one machine-wide daily allowance for
  everything standing (settings), not a per-order negotiation. Per-order caps
  exist only as *proposals the product makes* when one order hogs the pool.
- **CHEAP EYES, SMART JUDGMENT, ORDINARY HANDS.** The sentinel (watching) stays
  on the low tier. The new *judgment* role — deciding what an event means and
  whether to act — sits on the high tier. The work an order launches runs as an
  ordinary task with the ordinary audit gate.

## Decisions

**D1 — Altitude.** `Altitude` on `Item`: `conversation` | `project` |
`machine`. The zero value reads as `project`, because every item made before
altitudes were spelled was workspace-scoped; nothing migrates, nothing breaks.
A `conversation` order requires `Origin.SessionID` and dies with its
conversation. A `machine` order keeps the existing convention (workspace = the
person's home).

**D2 — Brief.** `Brief{Title, Prompt}` on `Item`. Title is for rows too narrow
for a sentence; Prompt is the compiled working instruction the machinery
follows. Both empty reads as `Words`. Shown together everywhere the item is
opened — words as the epigraph, brief as the mechanism.

**D3 — Grant.** One prose sentence on `Item`: what acting on this order may do
without asking. Empty means say-only (every pre-existing item). Wave 1 stores
and displays it; wave 3's judgment is bounded by it.

**D4 — Exceptions.** `[]Exception` on `Item`, each naming exactly one
workspace OR one session. Made by the person only, from either direction (the
place, or the order's record); both gestures write the same fact. Displayed
under the emptiness law: no exceptions, no ink.

**D5 — Resolution.** `Item.Level()`, `Item.AppliesTo(workspace, sessionID)`,
`Item.ExceptedFrom(...)` are pure predicates in the contract.
`Store.Applicable(workspace, sessionID)` is the one resolver every seam calls:
active items whose altitude reaches the place, minus exceptions, ordered
conversation → project → machine, recent first within a shelf.

**D6 — Default altitude is where it was said.** Said in a chat → `project`
(matching today's behavior — a chat is usually about its project); said at
home's box → `machine`; scope words ("just this conversation", "everywhere",
"all my projects") move it; the card always states it and `change` renegotiates
it.

**D7 — The page.** `/standing` opens **standing orders** — one page, up to
three shelves (`in this conversation` / `for this project` / `everywhere`),
each row leading with the title (or clipped words), glyph, and a derived status
clause. Row verbs: `enter` opens the origin conversation, `s` stand down
(retire), `p` pause/resume, `n` not here (exception at the opened altitude).
An empty shelf draws nothing.

**D8 — The threshold line.** Opening a conversation prints one dim line when
orders govern it: `· 3 standing orders here — /standing`. Zero orders, no line.
(The resume-delta variant — "new since you were here" — is a later wave.)

**D9 — Awareness ladder (later waves).** Rail: one folded breathing line, not
a list. The order's own room (full record, editable brief, clickable scope
map). Home bands gain order rows grouped by altitude.

**D10 — Enforcement seams (later waves).** Exactly three: **birth** (JIT brief
assembly for new tasks/turns appends applicable clauses), **landing** (a
post-merge event runs sentinel → judgment → possibly an admitted fix task
carrying the order's provenance), **tick** (already exists). Wired through one
optional door in the session config; nil means every surface draws nothing and
every tool is absent — the capability is absent, not broken.

## Requirements and waves

| R | What | Wave |
|---|------|------|
| R1 | Item fields (altitude, brief, grant, exceptions) + validation + compat | **W1** |
| R2 | Resolver + predicates + exceptions, both directions | **W1** |
| R3 | `stand` tool + card carry altitude/brief/grant; card names altitude | **W1** |
| R4 | `/standing` page (three shelves, verbs) | **W1** |
| R5 | Threshold line | **W1** |
| R6 | Birth seam: applicable clauses into new work's world | W2 |
| R7 | One-pool money: defaults from settings, card stops quoting per-run | W2 |
| R8 | Landing seam + judgment role (high tier) + acting under grant + provenance news | W3 |
| R9 | Order's room: record, editable brief (down free / up re-ratifies), scope map | W4 |
| R10 | Rail breathing line; home band altitude grouping; built-in orders; phone | W4–W5 |

Manual pages ride **every** wave (the manual law), never a wave of their own.

## Wave 1 TODO and dependencies

- [x] Contract: Item fields + predicates + Validate, hand-written first (this commit)
- [x] Contract: session seam stubs (`Agent.StandingHere`, `StandingExcept`, `StandingStandDown`, `StandingPause`) (this commit)
- [ ] Lane `so/engine`: resolver + store verbs + seam bodies + `stand` tool altitude/brief/grant + card + tests + manual (`stand`'s page sections)
- [ ] Lane `so/page`: `/standing` page + keys + manual page `standing-orders.md` + gate strings
- [ ] Lane `so/threshold`: threshold line + test (builds against the seam stub)
- [ ] Merge order: engine → page → threshold; full suite; `make build`; push; worktrees removed

Lane `so/page` and `so/threshold` depend only on the seam signatures, which this
commit freezes. A field or method added mid-build is a change every lane must
hear about; prefer finishing a wave and amending the contract between waves.
