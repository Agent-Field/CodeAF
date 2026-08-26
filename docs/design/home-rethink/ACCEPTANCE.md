# Acceptance — the home rethink, walked on a machine with things on it

2026-08-25, lane OPENS-acceptance. Every block below is `tmux capture-pane -p`, verbatim,
from `bin/aforge` built by `make build` at this branch's tip and run against a **throwaway
fixture home** — never the owner's `~/.aforge`. Long runs of blank frame rows are collapsed
to `… N blank rows …` and nothing else is edited.

## The fixture this was walked on

Built by a throwaway Go program that writes the real on-disk shapes through
`internal/session`, `internal/standing` and `internal/store` — never hand-rolled JSON — so
what the surface read is what the product's own writers produce.

| What | How much |
| --- | --- |
| projects | 3 — `~/aforge-v2`, `~/pricing-site`, `~/infra` |
| conversations | 12, three of them archived; one stopped on a question in its `presence.json`, one holding a running task |
| task index | 7 rows across the three buckets — one running, four landed, one failed 8 days back, two landed today |
| standing orders | 4 in `~/.aforge/v3/standing/*.json` — one on `asks first`, one fired today with a check line and `earning trust 3/5`, one paused, one rule (`holds`) |
| memory | 10 memories over the three shelves, in `~/.aforge/graph.db`, with every conversation's turns in the searchable index |
| usage ledger | 42 lines over 14 days across three models and four roles, in `~/.aforge/v3/usage.jsonl` |

One thing the fixture had to learn: **a project under `/tmp` is litter to the launch sweep**
(`internal/session/sweep.go`'s `sweepTTL`), so the first build lost its two oldest archived
conversations and the failed task with them. The projects live under the fake HOME instead.
That is a fact about writing fixtures, not a defect.

## The verdict table

| Screen | What it is | Verdict |
| --- | --- | --- |
| 1a | the flat list at 120 columns | **matches** |
| 1c | 80 columns, quiet morning | **matches** |
| 1d | 200 columns, the card acts | **differs** — the card draws four of the design's five bands; `it is stopped on you` with `y yes · n no` is not drawn for a question another window is holding, and `made for you` needs a deliverable the fixture has none of |
| 1e | tasks | **matches**, with two wording differences noted below |
| 1g | typing offers places | **differs** — the place is offered and says `a place`, but it ranks LAST rather than first |
| 2b | home on the scale, seven places, global composer | **matches** |
| 2c | spend | **differs** — the role column is the role each CALL named, not the crew binding (FIDELITY item 7); model ids are drawn raw; the `what it was for` block is absent because no ledger line in the fixture carries a task, session or standing id |
| 2d | memory at scale | **matches** at this scale; the design's `wants your eye` and `gaps it knows it has` blocks are **not built** (FIDELITY has no mechanism behind either) |
| 2f | standing, and how much rope | **matches** — three rope states, the design's mechanism, the word `trust` per FIDELITY's flagged deviation |
| 3b | the map | **matches**, as `alt+.` rather than a held modifier — FIDELITY's first flagged deviation |
| 3c | the verb strip | **differs** — the strip is drawn at the FOOT, under the composer, rather than pushing the list down at the row |
| 3d | the time window on the arrow axis | **matches** on spend and standing; on tasks the keys work and **no control is drawn** — see the last section |

## 1a · the flat list, 120 columns

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

10 chats · what wants you first                                       alt+g group by project · alt+q hide the quiet ones
? The Tab Bar's Counts                                      aforge-v2 asks: which of the two folds should stay open? 40m
⠸ The Certificate Rotation                                                                      infra 1 task running 20m
○ Porting the Picker                                                                                        aforge-v2 2m
○ Why the Frame Jumps                                                                                       aforge-v2 3h
○ Pricing Research                                                                                       pricing-site 3h
○ The Annual Toggle                                                                                      pricing-site 5h
○ Reading the Ledger                                                                                        aforge-v2 1d
○ What the Discount Means                                                                                pricing-site 1d
▸ 2 more, quiet since aug 23
        … 23 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 type to search or start something new · ↑↓ pick · enter open · tab next place
```

**Matches.** The pulse's clauses are the design's words (`1 want you · 1 moving`), the mark
per state is `?` / the spinner / `○`, the project is a tag on the row, the fold says
`▸ 2 more, quiet since aug 23`, and the foot is 1a's sentence word for word. The design's
`since you left` ledger is absent because nothing on this fixture happened on its own while
nobody was looking — the emptiness law, working.

## 1c · 80 columns, a quiet morning

Captured with the two live conversations' presence files allowed to go stale, which is what
a machine looks like when nothing is running and nothing is asking.

```
 aforge                                            $2.60 / $20.00 · tue 10:48pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────

○ Porting the Picker                                                aforge-v2 4m
○ The Certificate Rotation                                             infra 22m
○ The Tab Bar's Counts                                             aforge-v2 42m
○ Why the Frame Jumps                                               aforge-v2 3h
○ Pricing Research                                               pricing-site 3h
○ The Annual Toggle                                              pricing-site 5h
○ Reading the Ledger                                                aforge-v2 1d
○ What the Discount Means                                        pricing-site 1d
▸ 2 more, quiet since aug 23
        … 8 blank rows …
────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                      here ~/aforge-v2
 type to search or start something new · ↑↓ pick · enter open · tab next place
```

**Matches.** No ledger, no accent, no headings, no `want you` or `moving` clause — every
row is `○`, and eight rows plus a fold is the whole screen. The money segment survives
because this fixture HAS spent money today; on a machine that has spent nothing the clause
is absent and the line is `aforge` and the clock, which is what 1c draws.

## 1d · 200 columns, the card

```
 aforge                                                                                                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:48pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

10 chats · what wants you first                                                                   alt+g group by project · alt+q hide the quiet ones    The Tab Bar's Counts
? The Tab Bar's Counts                                                                  aforge-v2 asks: which of the two folds should stay open? 41m
⠇ The Certificate Rotation                                                                                                  infra 1 task running 21m    …ge-v2 · open in another window · waiting on you
○ Porting the Picker                                                                                                                    aforge-v2 3m
○ Why the Frame Jumps                                                                                                                   aforge-v2 3h    work
○ Pricing Research                                                                                                                   pricing-site 3h    ✓ count the tabs                           $0.42
○ The Annual Toggle                                                                                                                  pricing-site 5h
○ Reading the Ledger                                                                                                                    aforge-v2 1d    touched 3 files · spent $0.84 · 440k tokens
○ What the Discount Means                                                                                                            pricing-site 1d    last active 41m
▸ 2 more, quiet since aug 23
                                                                                                                                                        → verbs: put it away, new chat here, open folde…
        … 27 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                              here ~/aforge-v2
 type to search or start something new · ↑↓ pick · enter open · tab next place
```

**Differs.** Four of the design's five bands are there — the title, the place line
(`…ge-v2 · open in another window · waiting on you`), `work` with `✓ count the tabs $0.42`,
the facts line (`touched 3 files · spent $0.84 · 440k tokens`), and `→ verbs: …`.

Two are not:

- **`it is stopped on you`, the question, and `y yes · n no`.** The row IS asking — the card
  says `waiting on you` — but the question belongs to a conversation ANOTHER window is
  holding, and this window cannot answer somebody else's askback. The design draws a card
  that can. Not built; it needs a seam that carries an answer to another process.
- **`made for you` with the path.** No conversation in the fixture produced a deliverable,
  so under the emptiness law the band is absent. Correct, and untested here.

The card column is also narrower than the design's, so its lines cut with `…`.

## 1e · tasks

```
 aforge                                                                                                     tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

work aforge ran on its own. 7 since aug 12, $2.98 of it.

running
› ◐ rotate the staging certificate                                                          the certificate rotation now

done today
  ✓ count the tabs                                   the tab bar's counts 3 files · each tab wears what changed $0.42 3h
  ✓ port the picker                             porting the picker 7 files · the picker reads the registry now $1.63 30m
  ✓ price the tiers                          pricing research 2 files · three tiers, with the middle one marked $0.55 4h

earlier
  ✕ the fold spike                                                      an old spike on folds gave up, said why $0.05 8d
  ✓ move the runner pool                         moving the runner pool 4 files · eight runners on the new pool $0.21 1d
  ✓ the annual toggle                                      the annual toggle 1 file · annual is the default now $0.12 1d
        … 18 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 1 running · 3 done today · 3 earlier
 › say what you want done                                                                              here ~/aforge-v2
 enter go inside it · type to filter · tab next place
```

At 80 columns the middle clause gives way and the money and age stay:

```
 aforge                                                             tue 10:47pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────

work aforge ran on its own. 7 since aug 12, $2.98 of it.

running
› ◐ rotate the staging certificate                  the certificate rotation now

done today
  ✓ count the tabs                                 the tab bar's counts $0.42 3h
  ✓ port the picker                                 porting the picker $1.63 31m
  ✓ price the tiers                                    pricing research $0.55 4h

earlier
  ✕ the fold spike                                an old spike on folds $0.05 8d
  ✓ move the runner pool                         moving the runner pool $0.21 1d
  ✓ the annual toggle                                 the annual toggle $0.12 1d
        … 18 blank rows …
────────────────────────────────────────────────────────────────────────────────
 1 running · 3 done today · 3 earlier
 › say what you want done                                      here ~/aforge-v2
 enter go inside it · type to filter · tab next place
```

**Matches.** Grouped by what you do next, the section words are the design's
(`running`, `done today`, `earlier`), the failed row wears `✕` and `gave up, said why`, the
head sentence is 1e's (`work aforge ran on its own. 7 since aug 12, $2.98 of it.`) and the
count line sits just above the composer.

Two wording differences, both deliberate in the code: the foot says `enter go inside it`
where the design says `enter open its room` (the row under the cursor is work this
conversation is not holding, so there is no room to open), and `→ verbs: run it again, stop
it` is absent for the same reason — the place offers `s stop it` only over work this
conversation holds, and a foot may not name a key that does nothing.

## 1g · typing offers places

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:47pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        … 19 blank rows …
  aforge-v2
  ? The Tab Bar's Counts                                                                            waiting on you · 40m

  pricing-site
  ○ The Annual Toggle                                                                                        1 task · 5h
  ○ Pricing Research                                                                                         1 task · 3h
  ○ What the Discount Means                                                                                           1d

  infra
  ⠧ The Certificate Rotation                                                                             1 running · 20m

  ▸ standing                                                                                                     a place
  ? ask here: "sta"
› + start a new conversation: "sta"

────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › sta                                                                                                 here ~/aforge-v2
 enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · tab next place · esc clear
```

**Differs in one respect.** The place is offered, wears `▸`, and says `a place` at the right
margin exactly as 1g draws it — but it is the LAST row of the drop-up, under the matching
conversations, and the design says "Places rank first when the words match". Not fixed here:
the ranking is `switcher.go`'s and changing it is a reading-layer change with its own tests,
not a wrong word.

## 2b · the composer and the scope chip, on every place

The chip is on the box row of every place, and it says where what you type will land. On
home it is the project the cursor is standing on; everywhere else it is this window's own.

On home the box row reads `› say what you want done` … `here ~/aforge-v2`. On spend, with
something typed into it:

```
 aforge                                                                                                     tue 10:47pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

aug 12 – aug 25 · $5.94 · 1.1M tokens                                                  shift+←→ window · shift+↑ coarser
⣦⣄⣿⣷⣦⣄⣿⣷⣦⣄⣿⣷⣦⣄
aug 12                                                                                                       today $0.18
aug 14 was the loudest day — $0.72

what ran it · by the model, and the role it named
· anthropic/claude-opus-4-1 · standing ████████████ 9 calls · 116.1k                                               $0.66
· anthropic/claude-sonnet-4-5 · task ███████████ 9 calls · 116.1k                                                  $0.60
· anthropic/claude-opus-4-1 · chat ██████████ 7 calls · 90.3k                                                      $0.57
· anthropic/claude-sonnet-4-5 · chat ██████████ 6 calls · 77.4k                                                    $0.54
· deepseek/deepseek-v4-flash-latest · chat ██████████ 6 calls · 77.4k                                              $0.54
· deepseek/deepseek-v4-flash-latest · title █████████ 11 calls · 141.9k                                            $0.51
· anthropic/claude-opus-4-1 · task █████████ 6 calls · 77.4k                                                       $0.48
· anthropic/claude-sonnet-4-5 · standing █████████ 6 calls · 77.4k                                                 $0.48
· deepseek/deepseek-v4-flash-latest · standing █████████ 6 calls · 77.4k                                           $0.48
· deepseek/deepseek-v4-flash-latest · task █████████ 6 calls · 77.4k                                               $0.48
· anthropic/claude-opus-4-1 · title █████ 6 calls · 77.4k                                                          $0.30
· anthropic/claude-sonnet-4-5 · title █████ 6 calls · 77.4k                                                        $0.30
        … 15 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › cut the opus                                                                                        here ~/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

**Matches.** **Fixed in this lane:** home's chip drew the raw absolute path
(`here /home/santosh/af-acc-home/aforge-v2`) while every other place drew the short form, so
one fact was spelled two ways on two frames a `tab` apart. Home now shortens it the way
`app.placePath` already did, and both read `here ~/aforge-v2` — the design's own spelling.

**What is NOT built is SCREEN 2e**, and the capture above is the evidence: typing on spend
gives you the composer with its chip, and none of 2e's layer — the page behind does not dim,
there is no `it will run on its own and tell you when it lands` lead with `a task`
right-flushed, none of the three facts (`· in ~/aforge-v2, on master`, `· execution runs on
opus 4.1`, `· it may spend up to $2.00 before it asks`), and the foot is the router's line
rather than `alt+enter send it off · enter talk about it first · esc back to spend`.

## 2c · spend

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

aug 12 – aug 25 · $5.94 · 1.1M tokens                                                  shift+←→ window · shift+↑ coarser
⣦⣄⣿⣷⣦⣄⣿⣷⣦⣄⣿⣷⣦⣄
aug 12                                                                                                       today $0.18
aug 14 was the loudest day — $0.72

what ran it · by the model, and the role it named
· anthropic/claude-opus-4-1 · standing ████████████ 9 calls · 116.1k                                               $0.66
· anthropic/claude-sonnet-4-5 · task ███████████ 9 calls · 116.1k                                                  $0.60
· anthropic/claude-opus-4-1 · chat ██████████ 7 calls · 90.3k                                                      $0.57
· anthropic/claude-sonnet-4-5 · chat ██████████ 6 calls · 77.4k                                                    $0.54
· deepseek/deepseek-v4-flash-latest · chat ██████████ 6 calls · 77.4k                                              $0.54
· deepseek/deepseek-v4-flash-latest · title █████████ 11 calls · 141.9k                                            $0.51
· anthropic/claude-opus-4-1 · task █████████ 6 calls · 77.4k                                                       $0.48
· anthropic/claude-sonnet-4-5 · standing █████████ 6 calls · 77.4k                                                 $0.48
· deepseek/deepseek-v4-flash-latest · standing █████████ 6 calls · 77.4k                                           $0.48
· deepseek/deepseek-v4-flash-latest · task █████████ 6 calls · 77.4k                                               $0.48
· anthropic/claude-opus-4-1 · title █████ 6 calls · 77.4k                                                          $0.30
· anthropic/claude-sonnet-4-5 · title █████ 6 calls · 77.4k                                                        $0.30
        … 15 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

**Differs in three ways, and the third is FIDELITY's.**

1. **The role column is per-call, not the binding.** The caption reads `by the model, and the
   role it named` and each row is one (model, role) pair out of the ledger. FIDELITY item 7
   asks for the CURRENT crew binding — one row per model, joined against
   `config.ModelSlots`, with `planning` drawing `unbound · follows execution`. Not built.
2. **Model ids are raw** (`anthropic/claude-opus-4-1`), where the design draws `opus 4.1`.
3. **`what it was for` is absent.** The block exists in the reading; no line in this
   fixture's ledger carries a task, session or standing id, so under the emptiness law there
   is nothing to draw. Untested here rather than missing.

The window header, the sparkline, the `today $…` right-flush and the loudest-day line are
all there. The sparkline is braille rather than the design's block ramp.

## 2d · memory

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

10 held · 3 shelves                                                                                       type to filter

shelves · biggest first                              fact 4 · preference 3 · decision 1 · correction 1 · project state 1
▾ you · 4  mostly preferences · 4 new today                                                                          now
· not tabs  correction  new, learned now                                                                             now
· prose in comments  preference  new, learned now                                                                    now
· works in the evening  fact  new, learned now                                                                       now
▸ 1 more, on this shelf
▸ this project · 3  mostly fact · 3 new today                                                                        now
▸ this machine · 3  mostly facts · 3 new today                                                                       now
        … 23 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 enter open a shelf · → verbs · ↑↓ move · type to filter · alt+s walk the shelves · tab next place · esc close
```

**Matches at this scale.** Shelves biggest first, the biggest one open and the rest rolled
up, the kind legend on the section line, the counts exact, `▸ 1 more, on this shelf`, and
the foot naming every key that is real.

**Fixed in this lane, twice.** The kind ran straight into the title — a line read
`· not tabscorrection`, two facts glued into a word that is neither — and now takes the
shelf row's own two-cell lead. And the store's fifth kind was drawn as `project_state`,
which is a column name; it reads `project state` now, in the legend and on the row
(CLAUDE.md's no-machinery-vocabulary law).

**Not built:** 2d's `wants your eye` block and its `gaps it knows it has` block. Neither has
a mechanism behind it — `store.Memory` has no unsettled/candidate state and no open-question
shelf — and FIDELITY's own deviation list holds the adjacent case (`project:<dir>` shelves,
absent because the store has no workspace column; the three shelves here are `you`,
`this project`, `this machine`).

## 2f · standing

```
 aforge                                                                                                     tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

  standing orders                                                                                       shift+← aug 25 →
  for this project
› ◦ tell me when CI goes red on aforge-v2                                              asks first · every twenty minutes
  everywhere
  ◦ always run gofmt before you say a change is done                                                               holds
  in other projects
  ◦ check the release feed every morning                             earning trust 3/5 · every morning at nine · last 4h
  ∙ remind me to write the weekly update                                                             asks first · paused
        … 25 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 enter open where it was asked · → pause · stop · not here · tab next place · esc
```

**Matches.** Four shelves each under its own heading and none drawn over an absence, and the
rope column is the design's mechanism exactly: `asks first` with no grant, `earning trust
3/5` with a grant and three clean firings, `holds` for the rule that never wakes, and the
paused order wearing `∙ … paused`. The word is `trust` and not `tenure`, which is FIDELITY's
own flagged deviation and CLAUDE.md's product-wall rule.

## 3b · the map

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  1 home   2 tasks   3 standing   4 memory   5 spend   6 search   7 settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

10 chats · what wants you first                                       alt+g group by project · alt+q hide the quiet ones
? The Tab Bar's Counts                                      aforge-v2 asks: which of the two folds should stay open? 40m
⠧ The Certificate Rotation                                                                      infra 1 task running 20m
○ Porting the Picker                                                                                        aforge-v2 2m
○ Why the Frame Jumps                                                                                       aforge-v2 3h
○ Pricing Research                                                                                       pricing-site 3h
○ The Annual Toggle                                                                                      pricing-site 5h
○ Reading the Ledger                                                                                        aforge-v2 1d
○ What the Discount Means                                                                                pricing-site 1d
▸ 2 more, quiet since aug 23
        … 23 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 alt+1…7 go to a place · alt+enter send it off as a task · → verbs on this row · esc close
```

**Matches**, as the chord `alt+.` rather than a held modifier — FIDELITY's first flagged
deviation, because a terminal cannot report a held key. Nothing moves: the tab words grow
their digits in the cells they were already in, and the hint line becomes the chord list.

## 3c · the verb strip

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

10 chats · what wants you first                                       alt+g group by project · alt+q hide the quiet ones
? The Tab Bar's Counts                                      aforge-v2 asks: which of the two folds should stay open? 40m
⠸ The Certificate Rotation                                                                      infra 1 task running 20m
○ Porting the Picker                                                                                        aforge-v2 2m
○ Why the Frame Jumps                                                                                       aforge-v2 3h
○ Pricing Research                                                                                       pricing-site 3h
○ The Annual Toggle                                                                                      pricing-site 5h
○ Reading the Ledger                                                                                        aforge-v2 1d
○ What the Discount Means                                                                                pricing-site 1d
▸ 2 more, quiet since aug 23
        … 22 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 a put it away   t new chat here   o open folder   c copy path
 esc or ← to leave · enter opens it instead
```

**Differs in where it is drawn.** The verbs are the row's own — this row has a folder, so it
offers all four — and every letter on the strip is a verb only while the strip is on screen,
which is the clause the whole key law turns on. But the strip is at the FOOT, under the
composer, where the design draws it inline at the row and pushes the list down by two rows.
The design's argument for the displacement is that it is what makes bare letters safe; the
foot's is that a place has one composer and the strip has to be beside it. Left as built.

## 3d · the time window

`shift+←` on spend pages the window back a fortnight, which on this fixture is a window
nothing was spent in — so the place says what it is for, which is the correct empty state:

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

 What this machine has cost, by the day, by the model, and by what it was
 for. Every model call writes a line, so the figures here are the bill and
 not an estimate. There is nothing to set here — the allowance is edited on
 the status line that shows it.
        … 29 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

`shift+←` on standing pages back one day and the order that fired today leaves the list. The
header — which is the control AND the reading — stays:

```
 aforge                                                                                                     tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

  standing orders                                                                                       shift+← aug 24 →
  for this project
› ◦ tell me when CI goes red on aforge-v2                                              asks first · every twenty minutes
  everywhere
  ◦ always run gofmt before you say a change is done                                                               holds
  in other projects
  ∙ remind me to write the weekly update                                                             asks first · paused
        … 26 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 enter open where it was asked · → pause · stop · not here · tab next place · esc
```

`shift+←` on tasks pages back a fortnight:

```
 aforge                                                                                                     tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

work aforge ran on its own. nothing since jul 29.
        … 32 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                              here ~/aforge-v2
 type to filter · tab next place
```

**Fixed in this lane.** Before it, tasks answered an empty WINDOW with the teaching prose
that belongs to an empty MACHINE — the count line went with it, and the count line is the
only thing on that frame naming the window the arrows move, so `shift+←` looked like the
page had been wiped and there was nothing on screen saying how to get back. The place now
tells the two apart (`tasksReading.held`) and keeps its head line, which says
`nothing since jul 29.` — in words, because the emptiness law forbids the `0` that sentence
used to draw.

**Still differs:** tasks has a window and draws **no control for it**. Standing and spend
each draw `shift+← <span> →` on their own header; tasks draws its head sentence with no
arrows, so four keys are bound and nothing on screen names them — which is the exact defect
`verbstrip.go`'s law was written against. Not fixed here: the head line is a reading-layer
sentence with its own tests and the fix belongs beside FIDELITY item 10, which also asks
memory for a window it does not have.

## The tab bar, at every width

At 80, 120 and 200 columns all seven words are on the bar with nothing dropped, and no row
of any place overflows its frame at any of the three. The fold ladder in
`app.placeTabBar` is for terminals narrower than 80.

80 columns:

```
 aforge                    1 want you · 1 moving · $2.60 / $20.00 · tue 10:47pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────

  Session   Context   Workspace   Display   Providers   Connections
────────────────────────────────────────────────────────────────────────────────

› ask before running                                                      prompt
    what happens when the model asks to run a tool. Dangerous shell commands
    are asked about whichever way this is set.
  tool exceptions                                                           none
  shell command rules                                                       none
  guardian                                                                   off
  approval countdown                                                          10
  starting a task                                                          sized
  check task work                                                             on
  who settles work that needs a look                                         ask
  memory                                                                      on
  task countdown                                                               5
  task repair rounds                                                           1
  tasks at once                                                         no limit
  busy machine                                                               1.5
  memory floor                                                              1536
  task model                                            follows the conversation
  session ceiling                                                             $0
  fallback models                                         nearest in the catalog
        … 10 blank rows …
────────────────────────────────────────────────────────────────────────────────
 saved to your profile · a project's own .aforge-v3/config.json is a hand edit
 › say what you want done                                      here ~/aforge-v2
 ↑↓ move · ←→ tabs · enter change · type to search · tab next place · esc close
```

## Every place opens — the thing the owner reported

The report was: `alt+2` tasks, `alt+3` standing and `alt+4` memory did nothing on a fresh
machine. Reproduced on an empty throwaway home before this lane's change, and gone after it.
On the fixture above every one of the seven opens with its data:

- `alt+1` home — 10 conversations, ranked
- `alt+2` tasks — 7 rows in four sections
- `alt+3` standing — 4 orders on three shelves
- `alt+4` memory — 10 memories on three shelves
- `alt+5` spend — 14 days, 12 model rows
- `alt+6` search — the teaching prose, then hits the moment anything is typed
- `alt+7` settings — its own second bar of sections

And search, with something typed into it:

```
 aforge                                                            1 want you · 1 moving · $2.60 / $20.00 · tue 10:46pm
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

aforge-v2 5 · pricing-site 4 · infra 3
› reading the ledger · I looked at the ledger and the fold in reading the ledger, and the registry is t… aforge-v2 · now
› an old dns wobble · I read the ledger and the numbers do not add up · 3 more in this chat                  infra · now
› moving the runner pool · I read the ledger and the numbers do not add up · 3 more in this chat             infra · now
› the certificate rotation · I read the ledger and the numbers do not add up · 3 more in this chat           infra · now
› what the discount means · I read the ledger and the numbers do not add up · 3 more in this chat     pricing-site · now
› copy for the hero · I read the ledger and the numbers do not add up · 3 more in this chat           pricing-site · now
› the annual toggle · I read the ledger and the numbers do not add up · 3 more in this chat           pricing-site · now
› pricing research · I read the ledger and the numbers do not add up · 3 more in this chat            pricing-site · now
› an old spike on folds · I read the ledger and the numbers do not add up · 3 more in this chat          aforge-v2 · now
› why the frame jumps · I read the ledger and the numbers do not add up · 3 more in this chat            aforge-v2 · now
› the tab bar's counts · I read the ledger and the numbers do not add up · 3 more in this chat           aforge-v2 · now
› porting the picker · I read the ledger and the numbers do not add up · 3 more in this chat             aforge-v2 · now
        … 20 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › ledger                                                                                              here ~/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

Two rows there carry no project tag (`an old dns wobble`, `an old spike on folds`). Both are
archived conversations, and the project a hit is labelled with is resolved from the world's
resting list, which archived rows have left. Noted, not fixed: it is `searchplace.go`'s
reading and a lane of its own.

## What this walk fixed, and what it left

Fixed while here, each because it was a wrong word, a missing separator or a fold that did
not fold — never a redesign:

| Where | What it was | What it is |
| --- | --- | --- |
| `pages.go` `app.scopeChip` | home drew `here /home/…/aforge-v2` while every other place drew the short form — one fact spelled two ways, one `tab` apart | both read `here ~/aforge-v2`, the design's own spelling |
| `memoryplace.go` the memory row | the kind ran into the title: `· not tabscorrection` | the kind takes the shelf row's two-cell lead: `· not tabs  correction` |
| `memoryplace.go` `memoryTypeWord` | the store's fifth kind drew as `project_state`, a column name on a person's screen | it reads `project state`, on the row and in the legend |
| `tasksplace.go`, `place_tasks.go` | an empty time WINDOW drew the teaching prose that belongs to an empty MACHINE, taking the count line — the only thing naming the window — with it | the two are told apart (`tasksReading.held`); the head line stays and says `nothing since jul 29.`, in words rather than as a `0` |

Left, with the reason:

- **The tasks place has a window and draws no control for it.** Four keys bound, nothing on
  screen naming them. The fix belongs beside FIDELITY item 10, which also owes memory a
  window it does not have; the head line is a reading-layer sentence with its own tests.
- **SCREEN 2e, the composer layer, is not built at all** — no dimmed page behind, no lead
  line, none of the three facts, and not its foot. It is a screen's worth of work.
- **SCREEN 2c's role column is per-call, not the crew binding** (FIDELITY item 7), and model
  ids are drawn raw where the design draws `opus 4.1`.
- **A place ranks last in home's typed drop-up** where 1g ranks it first. It is
  `switcher.go`'s ranking, with its own tests.
- **1d's `it is stopped on you` band** needs a seam that answers another window's askback.
- **2d's `wants your eye` and `gaps it knows it has`** have no mechanism behind them, and
  under the emptiness law a page may not draw the furniture of a feature it does not have.
- **A search hit from an archived conversation carries no project tag**, because the project
  is resolved from the resting list the archive line has left.
