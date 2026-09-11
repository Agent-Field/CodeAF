# Home, again — what the front page of aforge should be

*2026-09-10. Ideation, not a ruling. Read against the screenshot of home at 9:49am
(75 chats, two `?` rows, everything else quiet), the 2026-08-25 rethink
(`docs/design/home-rethink/`), the 2026-08-19 direction (`docs/home-design.md`)
and the two open groom notes (`ideation/home-input-groom.md`,
`ideation/questions-first-class.md`). Every technique below is named, mapped to a
concrete move on this screen, and given a verdict against the laws this codebase
already enforces. The owner filters; nothing here is built.*

---

## 0. The one-paragraph answer

Home today is a **directory** wearing a switcher's sort order: one flat list of 75
rows, a right column that spends 40% of the width on three facts, and a top
chrome that changes shape the moment you press enter. The 2026-08-25 rethink got
the *model* right — home is a glance, not a place you live; triage, door, recall;
events not inventories — and then shipped only the list. What is missing is
**zones**: a front page has a fixed reading order of small blocks, each block is
a door to the place that owns it, each folds, and each is absent when empty.
The proposal is five zones in one column (`since you left` · `wants you` ·
`moving` · `pick up` · `made for you`) over a folded `everything` list, a right
column that is a **preview** (the last exchange, not metadata) only where the
width earns it, and one head row family shared with the conversation so the
chrome never jumps. Nothing needs a new colour, a border, or a chart.

---

## 1. Audit — what the screenshot says

| # | What is on screen | Why it is a problem |
|---|---|---|
| 1 | `75 chats · what wants you first`, then **two** `?` rows and **73** quiet ones | The sort order is right and it produces a page that is 97% noise. "What wants you first" is a heading over a list that is almost entirely things that do not. The fold (`▸ 32 more, quiet since 5d`) starts at row 44 because the cap is the frame's room (`capAtRest`, `switcher.go:288`, floor `switcherShown = 8`): a taller terminal draws a longer wall. |
| 2 | Titles are model-written and collide: `Understanding Hash Tables in Data Structures` twice, `Hi`, `What Is This Image?`, `Selfcrit` | **Recognition over recall fails.** A title alone cannot tell two chats apart; the row needs a second discriminator. The row note exists (`switcherConversationNote`, `switcher.go:368`: `asks: …`, the consent line, `N tasks running · phase`, `N files made`) but it is written only for rows that need you, are moving, or made something since the look stamp — a quiet row, which is 73 of these 75, has no note at all, and below 80 columns nobody has one. |
| 3 | The project column is `af-final-ws4`, `-tmp-af-final-ws2`, `aforge-v2`, `~` | Test-fixture folder names are a data problem, but the column design assumed 3–5 named projects and gets 12 opaque slugs. `~` for "no folder" is a tilde the eye reads as a bullet. |
| 4 | Right column: title, path, `spent $0.0012 · 17.5k tokens · last active 12h`, verbs | Four lines in a 60-column region. **The card shows what pressing enter shows a beat later** (1a's own critique) and nothing that helps you decide *whether* to press enter. The band that would — `leftoff` (`homeband_leftoff.go`: the last thing you said, the last sentence it said) — is registered and **dead at rest**: the resting card is `homeSwitchCard` (`place_home.go:548`), five hand-picked bands, and `leftoff`, `state`, `news`, `nextup`, `repo` draw only on the card under a typed search (`home.go:5076`). |
| 5 | Every age is `10h`–`5d`; nothing is `moving` | A quiet machine draws exactly the same page as a busy one, minus two rows. There is no *rhythm* to the page: no "today", no "this week", no sense that time passed. |
| 6 | Box says `say what you want done`; foot says `type to search or start something new` | Two voices for one field (home-input-groom §1.1 owns the fix). |
| 7 | The top chrome | Home: `aforge … 2 want you · thu 9:49am` / places / rule / blank. Chat: blank / ` Home  ⟨chats⟩` / blank / rule. Same four rows, different contents in every one — see §6. |
| 8 | `2 want you` on the pulse, and the two `?` rows are the first two rows of the list | The number and the rows are eight columns apart and forty rows above the box that could answer them. The one thing home is *for* is not answerable on home. (`homeband_answer` draws chips only when the cursor is on that row and only for another window's consent.) |

**Inference, not fact:** the person in front of this screen opens it many times a
day for a few seconds each (the switcher premise). A page that costs a scan of 44
rows to find the row you left ten minutes ago is a page they will stop opening;
they will use ctrl+k instead, and home becomes the thing that appears at launch
and is escaped.

---

## 2. The frame — what this product is, in one table

| Fact | Consequence for home |
|---|---|
| aforge is **a session you sit in front of**; the resident is a different product | Home is not a dashboard. It draws nothing on its own, no charts, no telemetry. It is a glance. |
| Three jobs: **triage, door, recall** (`docs/home-design.md`) | Every zone must be one of the three, or it is not on home. |
| Four levels of reach: **machine · project · conversation · task** | The page reads top-down from machine facts to one conversation's facts. The cursor decides which reach the card is about. |
| The laws: emptiness, no machinery words, one accent, no borders, icons through `tokens`, headings dim, ground ladder of four steps | Every technique in §3 is tested against these. A "card" here is a block with a dim heading and a blank line above it, never a box. |
| Data home has and does not show (`home-concept-inventory.md` §3, §5): **memory**, the **machine-wide deliverables ledger**, **crew/effort**, **connected accounts**, **permissions posture**, **learned fixes**, **machines** | The candidates for new zones. Most belong on their own place; the ones that belong on home are the ones that *happened* (a deliverable was made, a memory was learned), not the ones that *exist*. |

---

## 3. The technique inventory

Every entry: the technique, what it would be on this screen, and a verdict —
**fits**, **fits with a twist**, or **does not fit here** with the reason. The
does-not-fits are kept in the table on purpose, so the next person does not
re-propose them.

### 3.1 Getting back to things (recall)

| Technique | On home | Verdict |
|---|---|---|
| **Recency chunking** (today · yesterday · this week · older — the shape every chat sidebar converged on) | Replace the one 75-row list with time chunks; each chunk a dim heading, each folds after 4–6 rows. | **fits.** It gives the page rhythm (§1.5) at zero new data — `LastActive` is already on the row. The fold rule `homeShown = 4` already exists per project; move it per chunk. |
| **Jump list / MRU** (a fixed-size "recent" band that is the fast path) | The 5–8 rows you were in most recently, *above* the chunks, one line each, no card. | **fits.** This is the switcher's actual working set. It is what ctrl+k already draws in a card; put it on the page. |
| **Stable slots / spatial memory** (things you return to often do not move) | `pin` as a verb: a pinned row sits at the top in the order you pinned it and never re-sorts on the 3s beat. | **fits with a twist.** Needs one bit in `meta.json`. Keep it to a handful; a pinned zone that grows is a second list. The twist: pins go *above* `wants you`? No — a question outranks a pin, always. Pins sit under `wants you` and `moving`. |
| **Recognition over recall** (a row is recognisable by more than its name) | Each row carries one dim note: where it stopped (`asked: which repo?`), what it made (`3 files`), or the last thing said (first 40 chars of the last user turn). Title collisions become harmless. | **fits.** The mockups had it (1a, 2b). The seam is `session.Peek` (last user line) and the artifacts ledger; both are read on the 3s clock already. |
| **Search as the universal recall** (type anywhere, results replace the page) | Already true. Add: places and verbs in the same results (`spend` opens the place; `new chat in ~/infra` is a row). | **fits.** The preamble of SCREENS.txt asked for it; not shipped. |
| **Breadcrumb of where you came from** (`esc` returns to the chat behind home; say so) | One dim row at the top of `pick up`: `you were in ⟨title⟩ · esc goes back`. | **fits.** The chat behind home is the single most likely thing you want; today nothing on the page names it except a ground on its row somewhere in the 75. |

### 3.2 Knowing what happened (triage)

| Technique | On home | Verdict |
|---|---|---|
| **Inbox zero / queue front** (the top of the page is answerable in place; answering pops the next) | `wants you` zone: each row is the question's own sentence plus its answers, digits answer, `enter` opens. Screen 1b. | **fits.** Today the answers are two doors away: chips on the card (`homeband_answer`, one shape — another window's consent) and letters on the `→` strip (`switcherQuestionVerbs`, `switcher.go:1064`, capped at 2). Draw the answers *on the row* for every question kind (questions-first-class owns the object); the strip stays for the long tail. |
| **Since-you-left ledger / receipts** (what the machine did on its own while you were away) | Exists (`addLedger`, `switcher.go:476`): standing items that fired, `learned N things, let go of M` → memory, `N tasks landed` → tasks. Silent on a zero look stamp and on this screenshot. Keep; it is the best teacher on the machine. Add **files made** (the artifacts ledger since the stamp) and **a fix reused** as line kinds. | **fits.** Decision 8 of LANES.md; memory landed, deliverables did not. |
| **Zeigarnik / open loops** (unfinished things nag; show them so they stop nagging) | `pick up` zone: conversations that ended with a question the model asked *you* and you never answered, tasks `incomplete`, a draft you typed and did not send. | **fits with a twist.** The engine knows the first two (task state; the last transcript turn is assistant and ends in `?`). Drafts per conversation are not persisted today — that is a seam, not a band. The twist: cap it at 3; an open-loops list that grows is guilt, not triage. |
| **Progress on the row** (`4 of 9`, `18 of 40`) for moving work | On the `moving` row, the frontier node's own line (`reading filings · 18 of 40`). | **fits.** The task rail has it; home's row does not. |
| **Honest urgency** (a clock that is real: `auto in 30s`, `$18 of $20 today`) | A consent on a clock shows the clock on the row; the pulse's money segment already anchors the day. | **fits.** Only the honest kind — see 3.5 for the kind that does not. |

### 3.3 Feeling the value (the "this is powerful" feeling)

This is the part the owner asked about by name. The honest version of every
growth trick is **proof of work**: show what the tool actually produced, in
the person's own terms, at the moment they come back. The dishonest versions
are streaks, badges, and confetti, and they are all listed as does-not-fit.

| Technique | On home | Verdict |
|---|---|---|
| **Proof of work / made-for-you** (the machine-wide deliverables ledger) | `made for you` zone: the last 3–5 files aforge produced, any conversation, newest first, each a door (`enter` opens the folder or the chat that made it). | **fits.** `~/.aforge/v3/artifacts.jsonl` exists, machine-wide, and is shown only per-conversation. This is the single strongest "it did something" signal the product owns and it is invisible. |
| **The week in one line** (endowed progress, stated calmly) | Under the pulse or at the foot of the page: `this week · 14 chats · 9 tasks landed · 6 files made · $3.40`. No streak, no comparison to last week, no arrow. | **fits with a twist.** Emptiness law: a week with nothing draws nothing. One line, dim, never a chart. It is *anchoring* the value against the cost on the same line, which is the honest form of the spend segment. |
| **Peak-end** (the last thing you see before leaving is the best thing it did) | The `made for you` zone sits *last* in the reading order, just above the box. | **fits.** Free; it is an ordering decision. |
| **Aha in the empty state** (a first-run home that shows three things to type) | Zero chats: the page is three example sentences you can press enter on, each in a different reach (`summarise this repo`, `remind me at 6 to leave`, `watch the pricing page and tell me when it changes`). | **fits.** Screen 1c drew the quiet empty; the *first-run* empty was never drawn. `firstrun.go` owns the door. |
| **Capability discovery by event** (you learn memory exists the day it learns something) | Already the law (preamble). Extend to fixes (`fixed a build error it had seen before`), harnesses (`ran a saved shape`). | **fits.** Additive lines in the ledger. |
| **Investment / IKEA effect** (the more you have taught it, the more it is yours) | A `memory` count on the tab already does this quietly (`memory 9`). Do not add more. | **fits as is.** |
| **Social proof** ("people also…", counts of other users) | — | **does not fit.** Single-person local tool; there is no "others". Inventing it would be a lie on screen. |
| **Streaks, badges, confetti, levels** | — | **does not fit.** Machinery vocabulary law, one-accent law, and the product's character ("a glance"). A tool you sit in front of is boring at rest on purpose. |
| **Variable reward / novelty feed** | — | **does not fit.** Home does nothing on its own; a feed that changes on every open trains checking, which is the opposite of triage. |
| **Scarcity / false urgency** (`3 tasks expiring`, countdown you did not set) | — | **does not fit.** Only clocks the person or the gate set are real (3.2). |

### 3.4 Reading the page (layout and cognition)

| Technique | On home | Verdict |
|---|---|---|
| **Fixed reading order** (zones in the same order every time, absent when empty) | The five zones of §4. A zone that is empty costs no row, not even a heading. | **fits.** It is `homebands.go`'s registry order applied to the *page* rather than the card. |
| **Chunking to 4–7** (Miller) | Each zone folds after 4–6 rows with `▸ N more`. The fold word names the place (`▸ 5 more · tasks`). | **fits.** `homeShown` exists. |
| **Progressive disclosure by width** | one column below `homeCardMin = 136` (`homebridge.go:101`); above it the right column becomes a preview. Never three columns. | **fits.** The ladder exists; the change is what the column *says*. |
| **Master-detail preview that is a preview** | The right column shows the **last exchange**: the last thing you said (2 lines), the last thing it said (4 lines), then made-for-you, then cost. For a task row: the frontier line and the last report paragraph. | **fits, and it is mostly built.** `leftoff` already reads the journal peek and draws `› ⟨last user line⟩` plus the reply's last sentence; it is simply not one of `homeSwitchCard`'s five. Add it, widen it to four lines, and move the facts line last. |
| **Whitespace rhythm** (blank line = section break, rows never get one) | Screen 2a's scale, applied. | **fits, already law.** |
| **One accent to the live thing** | Amber on the `?` row, the spinner on the `◐` row, nothing else lit. Headings dim. | **fits, already law.** |
| **Type-to-filter everywhere, letters never verbs** | Six-class key law (3a). Verbs via `→`. | **fits, already law.** |
| **Hover/cursor preview without commit** | Cursor moves the preview, enter commits; mouse hover previews. | **fits, already built** (`homehover`). |

### 3.5 Trust (the psychology that keeps a person using a tool that acts on its own)

| Technique | On home | Verdict |
|---|---|---|
| **Receipts with undo** (a model rewrote your memory: say so, offer `put it back`) | A `since you left` line kind for consolidation, with `u` on its strip. Screen 3e. | **fits with a twist.** Consolidation writes no receipt today (LANES decision 7). Seam first. |
| **Never delete, always `put it away`** | Archive is the only removal verb on home. | **fits, already true.** |
| **Esc means later, never no** | A question folded from home is still on the pulse count. | **fits;** questions-first-class owns it. |
| **The default is named and reasoned** (`my pick: 2 · it keeps the branch`) | On `wants you` rows with options. | **fits;** same owner. |

---

## 4. The layout — three candidates, one recommendation

Every mockup is a real column count. Glyphs are the `tokens` slots (`?` needs a
person, `◐` moving, `○` quiet, `✓` landed, `▸` folded, `›` the box). No borders,
no boxes; a "card" is a dim heading with a blank line above it.

### 4.1 Candidate A — zones over a folded list (recommended)

120 columns, the machine from the screenshot but with two questions, one task moving, three files made this week:

```
 aforge                                                          2 want you · 1 moving · $0.14 / $20.00 · thu 9:49am
 home   tasks 1   standing   memory 9   spend   search   settings
 ─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

 since you left · 12h
 · the 6am repo watch fired — nothing had changed, and it says so                                       standing
 · learned 2 things about aforge-v2                                                                       memory

 wants you
 ? Searching for Apartments Near 39 Minto Street         needs your ok to run bash · 1 yes  2 not now  enter open
 ? Clever Bet Prediction Model                           asked: which dataset, the 2024 one or both?      enter open

 moving
 ◐ Generate and Display First 200 Primes                 running tests · 2 of 3                  af-stop-ws   4m

 pick up
 · Understanding Hash Tables in Data Structures          you were here · esc goes back           af-wn-ctl    12h
 · Locate Recent Sandbox Task in Browser History         it asked: keep the sandbox or tear it down?          13h
 · Spark Fleet Ssh Audit                                 incomplete · 1 task stopped, said why                  1d

 made for you
 ✓ reports/apartments-minto-street.md                    Searching for Apartments                              11h
 ✓ primes.py                                             Generate and Display First 200 Primes                 10h
 ✓ lighthouse-analysis.md                                Analyzing the Image                                   15h

 today
 ○ Understanding Bloom Filters in Eight Sentences                                                 af-final-ws4  11h
 ○ Hash Table Data Structure Explanation                                                          af-final-ws3  11h
 ○ AI Influencers and Developers in Agent Field                                                                 11h
 ○ First Line: Casual Greeting Exchange                                                          -tmp-af-final  11h
 ▸ 9 more today

 ▸ yesterday 14 · this week 22 · older 32                                          ⌥g by project · ⌥q hide the quiet

 ─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                          here ~/aforge-v2
 type to reach anything · ↑↓ pick · enter open · 1 2 answer · tab next place
```

Reading order and the rule for each zone:

| Zone | What it is | Order | Folds at | Absent when |
|---|---|---|---|---|
| `since you left` | the ledger — what happened on its own since the look stamp | newest first | 3 | nothing fired, learned, or landed |
| `wants you` | every question, any asker, answerable on the row | oldest first (the one that has waited longest is first) | never — it is the page's reason | no question |
| `moving` | everything in flight, one spinner | most recently touched first | 4 | nothing running |
| `pick up` | the chat behind home; chats whose last turn is a question to you; incomplete tasks | the chat behind home first, then newest | 3 | none of those |
| `made for you` | the machine-wide deliverables ledger | newest first | 3 | nothing made in 7 days |
| `today` / `yesterday` / `this week` / `older` | the rest, recency-chunked | newest first | 4 per chunk, and chunks past `today` fold whole | — |

Why this shape:

- **The first two screens' worth are the three jobs in order:** what happened,
  what needs me, what is running (triage); where I was (recall); what it made
  (the value); then everything (recall by search).
- **On a quiet machine it collapses to screen 1c.** No ledger, no questions, no
  moving, one `pick up` row (the chat behind home), maybe one file, then `today`.
  Eight lines is a legitimate home.
- **Every zone is a door.** Heading `enter` or the fold opens the place that owns
  it: `since you left` → the line's place, `moving` → tasks, `made for you` → the
  folder, chunks → search with that window.
- **Nothing here needs a new colour.** Amber on `?`, the spinner on `◐`, the
  cursor ground, and dim everywhere else.

### 4.2 Candidate B — the owner's "boxes": zones side by side

The owner's sketch: one region for recent things to move between, one for a
preview, one for spend or what needs you. At 200 columns it is candidate A's
zones laid in two columns:

```
 aforge                                                                                      2 want you · 1 moving · $0.14 / $20.00 · thu 9:49am
 home   tasks 1   standing   memory 9   spend   search   settings
 ─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

 wants you                                                            │ Searching for Apartments Near 39 Minto Street
 ? Searching for Apartments Near 39 Minto Street    needs your ok     │ ~/scratch/apartments · here
 ? Clever Bet Prediction Model                      asked: which one? │
                                                                      │ you said
 moving                                                               │   find 2-beds within 15 min walk of the park, under 2.4k
 ◐ Generate and Display First 200 Primes            tests · 2 of 3    │ it said
                                                                      │   Three candidates on Minto and one on Bain. The Bain one
 pick up                                                              │   is the only one with a photo of the actual unit. Before
 · Understanding Hash Tables                        you were here     │   I pull the listings I need to run a shell command —
 · Locate Recent Sandbox Task                       asked: keep it?   │
                                                                      │ needs your ok to run bash
 made for you                                                         │   curl -s https://…/listings.json
 ✓ reports/apartments-minto-street.md               11h               │   1 yes · 2 not now · a always for curl · enter open
 ✓ primes.py                                        10h               │
                                                                      │ made for you
 today                                                                │   reports/apartments-minto-street.md
 ○ Understanding Bloom Filters                      af-final-ws4  11h │
 ○ Hash Table Data Structure Explanation            af-final-ws3  11h │ spent $0.31 · 41k tokens · thinking medium
 ▸ 11 more today                                                      │ → verbs: put it away · new chat here · open folder
 ▸ yesterday 14 · this week 22 · older 32                             │
```

(The `│` is only in this sketch. The real gutter is two blank columns, as
`homebridge.go` draws today.)

Verdict: **B is A at ≥136 columns.** The zones do not move; the right column
becomes a preview *of the row under the cursor* and, with the cursor on nothing,
the week in one paragraph. Two side-by-side lists of different things (recent on
the left, spend on the right) were tried in the three-column era and retired
because the eye has to choose a column before it can read; one reading order
beats two.

### 4.3 Candidate C — the queue front (screen 1b)

The top of the page *is* the first question, in full, with its answers; the
rest of the page is `then`. Verdict: **C is what `wants you` becomes when a
question is long.** A one-line question sits on its row (A); a question with a
paragraph gets the row *and* the preview column (B). It is not a third layout.

### 4.4 What is retired

- The dead half of the band registry. Ten registered bands have no resting call
  site (`state`, `leftoff`, `news`, `nextup`, `repo`, `gone`, `spend`, `keys`,
  `thinking`, `work`'s registry copy) and the project card (`projectfacts`:
  `12 conversations · 34 tasks · spent $4.10`) is computed for a row kind the
  switcher never emits. Either they come back as zones and preview lines, or
  they go; a registry that is 60% unreachable is a band-aid by omission.
- The **card as metadata**. `spent · tokens · last active` moves to the last line
  of the preview; the preview leads with the exchange.
- **Per-project grouping as the default.** `⌥g` keeps it as a view.
- The **`~` project column** on a folderless row: draw nothing.

---

## 5. The preview column — what it should say

For a **conversation** row (the common case), top to bottom:

1. Title, path, `here` if it is this window's folder. (kept)
2. **`you said`** — the last user turn, 2 lines, fitted. (`leftoff` band, promoted)
3. **`it said`** — the last assistant turn's first 4 lines, or the question it is
   stopped on with its answers. (`leftoff` widened from one sentence to four
   lines; the question comes from `homeCardAnswer`, already there)
4. `made for you` — up to 3 files. (promoted from band 50)
5. `work` — running/landed/incomplete counts, one line. (band 40, compressed)
6. `spent $0.31 · 41k tokens · thinking medium`. (band 90/93, one line, last)
7. `→ verbs`. (kept)

For a **task** row: the brief, the frontier line, the last report paragraph,
cost, verbs.

For the **cursor on nothing** (machine): `this week` in one paragraph — chats,
tasks landed, files made, dollars — then `on watch` (standing count, next due:
today a healthy idle watch is invisible on home, `switcher.go:246` lists an item
only when it needs you or is running), then `it remembers 9 things about you ·
memory`. Every line a door.

**The law the preview must keep:** nothing on it may grow with the data. Every
list-shaped line folds at 3 and names its place.

---

## 6. The chrome — why it jumps, and one head row family

The two frames spend the same four rows and agree on none of them:

| Row | Home (`pages.go` `placeFrameWithBar`) | Conversation (`view.go` `chrome`, `chattabs.go` `tabsRow`) |
|---|---|---|
| 1 | pulse: `aforge` … `2 want you · thu 9:49am` | blank (`tabsLineRow`, on frames ≥48×32) |
| 2 | places: `home tasks standing memory spend search settings` | chats: ` Home  ⟨title⟩ ×  ⟨title⟩ …` |
| 3 | rule | blank (`tabsBottomPad`, on frames ≥48×36) |
| 4 | blank | rule (`chatRuleHeight`) |

So pressing `enter` on home makes row 1 go dark, row 2 change vocabulary, and
the rule drop one row. The place head is a constant (`placeHeadRows = 4`,
`placebodies.go:44`, consulted by the mouse at `placemouse.go:156`); the chat
head is adaptive (`tabsHeight`, `chattabs.go:463`: 80×40 → 4 rows, 80×34 → 3,
80×24 → 2, under 48 columns → 2). Below 60 columns home drops to a 2-row phone
head with no pulse and no places at all (`homephone.go:493`). So the two frames
agree only at big sizes, and even there not on what each row holds — which is the
height difference the owner named.

The tests that pin today's shapes, so a lane knows what it is rewriting:
`TestHeaderHomeAndPaddingAdaptWithoutLosingActiveTab` and
`TestHeaderAirDoesNotShrinkReadingWhenTerminalGrows` (`header_home_test.go`),
`TestTheHeaderPanelIsDrawnAndBudgetedAtEveryFrame` (`chattabclose_test.go:355`),
the `headHeight == tabsHeight + chatRuleHeight` identity in
`roomcrumb_contract_test.go:456` and `focus_test.go`, and on the place side every
test that presses at `placeHeadRows + 1` (`placemouse_test.go`,
`placeeverymouse_test.go`, `verbstrip_test.go:90`, `depthfade_test.go:145`).

Three ways to make it one family:

| Option | Shape | Cost |
|---|---|---|
| **1. The conversation gets the pulse row** (recommended) | Row 1 in a chat: `aforge` left, the same pulse right (`2 want you · 1 moving · $ · clock`). Row 2: ` Home  ⟨chats⟩`. Row 3: rule. Row 4: blank. Same rows, same rule position, and the pulse is now visible from inside a chat — which is where you are when a question lands in another window. | `pulseLine` is already a pure function of cached facts; `chrome` gains one row and loses its two blank pads. The status line at the foot keeps the conversation's own numbers; the pulse is the machine's. |
| 2. Home loses the pulse row | Fold the pulse into the tab row's right side: `home tasks … settings        2 want you · thu 9:49am`. Both frames become strip / rule / blank. | Loses one row of home chrome, which is good; but the places row is already full at 80 columns and the pulse has four segments. Falls apart narrow. |
| 3. One strip, two halves | Row 2 everywhere is `home ▸ tasks standing … │ ⟨chat⟩ ⟨chat⟩`: places on the left, open chats on the right, the current one lit. | Honest and dense, but it is the third tab vocabulary on one row and the chat titles are long. The eye cannot tell a place from a chat by shape. |

Option 1 also settles a question the home groom raised (§1.1, #18): nothing in a
chat says what the *machine* is doing. With the pulse on row 1 of every frame,
`2 want you` is one glance away from everywhere, and the `Home` word on the strip
is the door.

---

## 7. What each proposal needs from the engine (seams)

| Move | Data | Exists? |
|---|---|---|
| `made for you` zone, machine-wide | `~/.aforge/v3/artifacts.jsonl` | **yes**; only the per-conversation slice is read today |
| row note on a quiet row: last user line | the journal peek `leftoff` already caches per session | **yes**; the note function needs a fourth clause for quiet rows, and a 80-column floor already exists |
| `pick up`: chat ended on a question to you | last transcript turn is assistant and its last sentence ends `?` — or the question object once it lands | partly; the question object is the right seam |
| `pick up`: incomplete tasks | task index state | **yes** |
| `wants you` answerable on the row for every kind | the unified question object | **no** — `ideation/questions-first-class.md` |
| `since you left`: files made, fixes reused | artifacts since the look stamp; `fixes.json` counters | artifacts **yes** (memory is already a ledger line); fixes need a timestamp per reuse |
| the week line | `usage.jsonl` day aggregation + task index + artifacts | **yes** |
| pins | one bool in `meta.json` | **no**, trivial |
| unsent drafts in `pick up` | per-conversation draft persistence | **no**; a seam, not a band |
| the pulse on the conversation frame | `pulseLine` | **yes** |

---

## 8. What not to do (so it is written down)

- No borders, boxes or rules between zones. Blank line and dim heading only.
- No colour on a heading, ever; no second accent for "recent".
- No `$0.00`, no `0 files`, no empty zone with a heading.
- No chart on home. Sparklines belong to `spend`.
- No count that goes up to make you feel good (streaks, totals-all-time).
- No auto-refreshing feed. The 3-second beat re-reads; it never *animates* a
  change beyond the one spinner.
- No third column, at any width.

---

## 9. Open for the owner to settle

1. **Is `pick up` a zone or a sort key?** A zone is more legible; a sort key
   (open loops float to the top of `today`) is one fewer heading.
2. **`made for you` at 3, 5, or as a tab?** Three rows on home plus a place of
   its own (`made`) is the events-not-inventories answer. The tab bar is at
   seven already.
3. **Does the week line live under the pulse (machine reach, always) or on the
   machine preview (only with the cursor on nothing)?** Under the pulse is one
   row on every home; the preview is free.
4. **Pins: yes/no.** They are the only thing here that adds state.
5. **Chrome option 1, 2 or 3.** The recommendation is 1.
6. **What replaces the project column** when the folder is a fixture slug: the
   short path, the repo name from git, or nothing until `⌥g`?
