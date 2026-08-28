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

## The second walk — lane FIDELITY-2, 2026-08-26

The five screens the first walk marked **differs** were walked again against a fixture of
their own, built the same way and holding the two things the first one had nothing behind:
a conversation whose `presence.json` carries an **answerable question** another window is
holding, and a **file a conversation left behind** in the artifacts index. Their blocks
below are that walk's captures and replace the first walk's; every other screen's block is
the first walk's, untouched.

A second thing that fixture had to learn: **the projects root is `<aforge home>/v3/projects`**
(`internal/session/sweep.go`'s `placesDirName`), not `v3/places`. A fixture written to the
wrong folder opens on a home with one row on it — the conversation the launch itself made —
and looks exactly like a surface that cannot read the disk.

## The verdict table

| Screen | What it is | Verdict |
| --- | --- | --- |
| 1a | the flat list at 120 columns | **matches** |
| 1c | 80 columns, quiet morning | **matches** |
| 1d | 200 columns, the card acts | **matches** — all five bands, in the design's order, over a fixture holding an answerable question and a file |
| 1e | tasks | **matches**, with two wording differences noted below |
| 1g | typing offers places | **matches** — the place ranks first (nearest the box in an inverted column) and its margin says what is behind it |
| 2b | home on the scale, seven places, global composer | **matches** |
| 2c | spend | **matches** — the role column is the crew binding, models wear the word a person says, and `what it was for` names what it was for. One deviation stays and is flagged below: only the conversation slot can be asked about on this surface, so no `unbound` row is drawn |
| 2d | memory at scale | **matches** at this scale, item 5 included (`enter` on a line is `ask me about it`, the card behind `→ c`); the design's `wants your eye` and `gaps it knows it has` blocks are **not built** (FIDELITY has no mechanism behind either) |
| 2f | standing, and how much rope | **matches** — three rope states, the design's mechanism, the word `trust` per FIDELITY's flagged deviation |
| 3b | the map | **matches**, as `alt+.` rather than a held modifier — FIDELITY's first flagged deviation |
| 3c | the verb strip | **matches** — the strip is drawn under the row it acts on and pushes the list down by its own height |
| 3d | the time window on the arrow axis | **matches** on all three — one head row, drawn by one helper, with the label between the arrows |

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
 aforge                                                                                                                                             1 want you · 1 moving · $1.63 / $20.00 · wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

11 chats · what wants you first                                                                   alt+g group by project · alt+q hide the quiet ones    The Tab Bar's Counts
? The Tab Bar's Counts                 aforge-v2 wants to Add a --report-only mode so the report can be regenerated without re-running the sweep 40m
⠙ The Certificate Rotation                                                                                                  infra 1 task running 20m    …ge-v2 · open in another window · waiting on you
○ Porting the Picker                                                                                                                    aforge-v2 2m
○ Why the Frame Jumps                                                                                                                   aforge-v2 3h    it is stopped on you
○ Pricing Research                                                                                                                   pricing-site 3h    Add a --report-only mode so the report can be
○ Standing Up the Watches                                                                                                               aforge-v2 5h    regenerated without re-running the sweep?
○ The Annual Toggle                                                                                                                  pricing-site 5h    1 allow once · 2 always · 3 deny
○ Reading the Ledger                                                                                                                    aforge-v2 1d    enter open and talk
▸ 3 more, quiet since aug 24
                                                                                                                                                        work
                                                                                                                                                        ✓ count the tabs                           $0.42

                                                                                                                                                        made for you
                                                                                                                                                        swarm-decomposition.md                      · 2h

                                                                                                                                                        touched 3 files · spent $0.42 · last active 40m

                                                                                                                                                        → verbs: allow once, always, put it away, new c…
        … 18 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
› Add a --report-only mode so the report can be regenerated without re-running the sweep?                                                                               1 allow once · 2 always · 3 deny
 type to search or start something new · ↑↓ pick · enter open · tab next place
```

**Matches.** All five bands of the design, in the design's order: the title, the place line
(`…ge-v2 · open in another window · waiting on you`), `it is stopped on you` with the
question in its own words and the keys that answer it, `work` with `✓ count the tabs $0.42`,
`made for you` with the file, the facts line, and `→ verbs: …`.

Two of those the first walk could not see, and neither was a gap in the surface — the
fixture had nothing behind them. The question belongs to a conversation ANOTHER window is
holding, which is exactly the case this band exists for: the presence file carries an
answerable question, this window has somewhere to leave an answer, and the chips are the
ones that session said it would take. `made for you` needs a row in the artifacts index.
Both are now in the fixture and both draw.

**Fixed in this lane:** the design's answer row is `y yes · n no · enter open and talk`, and
the card drew the chips alone — so the third thing a person can do with a question they are
looking at was on no surface at all. It reads `1 allow once · 2 always · 3 deny · enter open
and talk` now, with the way out dim and the answers amber, because they are two different
offers.

The card column is narrower than the design's, so its longest lines cut with `…`.

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

> **The head line changed under 3d.** The window's edge is now said ONCE per frame: where the
> frame has room for the control, the span sits between its arrows and the sentence reads
> `work aforge ran on its own. 7, $2.98 of it.`; where it does not, the sentence keeps its
> `since aug 12` clause. The capture above is the first walk's and predates it — 3d's block
> has the current one.

Two wording differences, both deliberate in the code: the foot says `enter go inside it`
where the design says `enter open its room` (the row under the cursor is work this
conversation is not holding, so there is no room to open), and `→ verbs: run it again, stop
it` is absent for the same reason — the place offers `s stop it` only over work this
conversation holds, and a foot may not name a key that does nothing.

## 1g · typing offers places

```
 aforge                                                                                                                                             1 want you · 1 moving · $1.63 / $20.00 · wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        … 23 blank rows …
  pricing-site
  ○ The Annual Toggle                                                                                                                    1 task · 5h
  ○ Pricing Research                                                                                                                     1 task · 3h
  ○ What the Discount Means                                                                                                                       1d

  infra
  ⠦ The Certificate Rotation                                                                                                         1 running · 20m

  aforge-v2
  ? The Tab Bar's Counts                                                                                                        waiting on you · 40m
  ○ Standing Up the Watches                                                                                                                       5h

  ▸ standing                                                                                                       a place · 4 orders, 1 fired today
  ? ask here: "sta"
› + start a new conversation: "sta"

────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › sta                                                                                                                                                                             here ~/a/w/aforge-v2
 enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · tab next place · esc clear
```

**Matches.** The place is offered, wears `▸`, and says `a place · 4 orders, 1 fired today`
at the right margin.

**The first walk read the ranking wrong, and its own capture says so.** Home's column is a
DROP-UP: it is read UPWARD out of the box the words were typed into, and `home.go`'s law
over `buildWorld` states it in as many words — "a ranked list read UPWARD out of a box has
to put its best answer LAST, or the row somebody wants is the furthest one from the key they
reach for". The offered place sits BELOW every conversation the same three letters matched
and directly above `ask here`, which is the row nearest the reader's hand. That is the
design's "places rank first". Nothing about the ranking needed changing; what it needed was
a test, and `TestAPlaceOutranksEveryConversationTheWordsAlsoMatch` is it.

**Fixed in this lane:** the design's row is `▸ standing   a place · 6 promises, 1 fired
today` and the build stopped at `a place`. The clause is the place's own answer to "what is
behind you" (`place.summary`), taken on home's three-second beat and cached, never on a
draw. The noun is `orders` and not the design's `promises`: this place calls itself
`standing orders` on its own heading and the manual says it that way, and a second noun for
one thing is the drift the one-source-of-truth law is about. A machine with nothing standing
on it draws no clause at all, and a day nothing fired drops the firing half.

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

> **The spend capture above is the first walk's and its head row has changed under 3d.** The
> figures lead the left field and the span sits between the arrows; 2c's block has the
> current one. What this block is about — the composer and its chip on a place that is not
> home — is unchanged.

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
 aforge                                                                                                                                                                                      wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

$3.19 · 810.2k tokens                                                                                                                                         shift+← aug 13 – aug 26 →  shift+↑ coarser
⣦⣦⣦⣶⣶⣶⣷⣷⣷⣷⣿⣿⣿⣿
aug 13                                                                                                                                                                                       today $0.31
aug 26 was the loudest day — $0.31, porting the picker

what ran it · by the model, and the role it was bound to
· deepseek-v4-flash · conversation ████████████ 57 calls · 270.1k                                                                                                                                  $1.08
· claude-opus-4-1 ████████████ 56 calls · 270.1k                                                                                                                                                   $1.06
· claude-sonnet-4-5 ████████████ 55 calls · 270k                                                                                                                                                   $1.04

what it was for
· check the release feed every morning · standing · 45 firings                                                                                                                                     $0.85
· price-the-tiers · pricing-site · a task                                                                                                                                                          $0.82
· porting the picker · aforge-v2 · a conversation                                                                                                                                                  $0.79
▸ 1 more
        … 23 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

**Matches, with one deviation flagged below.**

1. **The role column is the crew binding** (FIDELITY item 7). The caption is the design's —
   `by the model, and the role it was bound to` — and the column now says it: each model is
   joined against `config.ModelSlots` as the settings stand right now, never against the
   auxiliary word one call gave itself. `session.UsageByModel` groups by the MODEL alone, so
   three opus lines, one of which named itself `title`, are one row. A model bound to
   nothing draws no role word at all.
2. **Model ids are gone.** A row is headed with the word a person says out loud —
   `claude-opus-4-1`, `deepseek-v4-flash` — through `modelui.ModelWord`, which is the
   spelling `/model`, the crew chips and the status line already use. The catalog's own
   display name was tried and dropped: it publishes `DeepSeek V4 Flash Latest` and
   `Google: Gemini 3.6 Flash`, so preferring it would make this the one surface on the
   machine calling a model something no other surface does.
3. **`what it was for` names what it was for.** The block was absent on the first walk
   because that fixture's ledger carried no ids; the engine's one ledger door stamps the
   conversation on every line it writes, so it is present whenever anything was spent. Two
   things had to be fixed for the names to arrive: the join read HOME'S cached world, which
   is dropped the moment home is left — and leaving home is how a person gets here, so every
   row wore a raw id — and the promises were asked of this window's project only. The place
   reads its own world on the way in and asks every project for its promises.
4. **A promise's row said `standing` twice** — `· release · standing · 45 firings ·
   standing` — because the tag already names the kind. The second one is gone.

**The deviation, flagged to the owner.** The design draws `· planning · unbound · follows
execution` for a slot with nothing bound to it, and the machinery for that row is built and
tested. It does not appear here, and the reason is not that planning is bound: **this window
holds a client for one of the five slots and answers the other four with the sentence
`app.slotRefusal` says** — "that model is chosen where its session is opened". So it cannot
tell "nothing is bound to planning" from "I have no way to ask", and the emptiness law says
an unknown is drawn as nothing rather than guessed at. The row appears the moment a door
wires `config.SettingsOptions.RoleModel`.

The window header, the sparkline, the `today $…` right-flush and the loudest-day line are
all there. The sparkline is braille rather than the design's block ramp. The head row's own
change is under 3d below.

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
 enter open a shelf · type to filter · alt+s walk the shelves · tab next place · esc close
```

**Matches at this scale.** Shelves biggest first, the biggest one open and the rest rolled
up, the kind legend on the section line, the counts exact, `▸ 1 more, on this shelf`, and
the foot naming every key that is real.

**Item 5 is built now, and the foot moved with it.** `enter` on a LINE is `ask me about it`
— the line opens a fresh conversation as its first message — and the card it used to open in
place is on the row's `→` strip behind `c`. The foot follows the row under the cursor, as
pages.go's contract for a hint has always asked: the shelf heading drawn above gets the line
in the frame, and a line gets 1f's own sentence,
`enter ask me about it · e fix the wording · f forget it`.

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
 aforge                                                                                                                                             1 want you · 1 moving · $1.63 / $20.00 · wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

11 chats · what wants you first                                                                   alt+g group by project · alt+q hide the quiet ones    The Certificate Rotation
? The Tab Bar's Counts                 aforge-v2 wants to Add a --report-only mode so the report can be regenerated without re-running the sweep 40m
⠦ The Certificate Rotation                                                                                                  infra 1 task running 20m    …e/work/infra · open in another window · working
 a put it away   t new chat here   o open folder   c copy path
○ Porting the Picker                                                                                                                    aforge-v2 2m
○ Why the Frame Jumps                                                                                                                   aforge-v2 3h    work
○ Pricing Research                                                                                                                   pricing-site 3h    ⠦ rotate the staging certificate
○ Standing Up the Watches                                                                                                               aforge-v2 5h      ◐ running
○ The Annual Toggle                                                                                                                  pricing-site 5h
○ Reading the Ledger                                                                                                                    aforge-v2 1d    last active 20m
▸ 3 more, quiet since aug 24
                                                                                                                                                        → verbs: put it away, new chat here, open folde…
        … 26 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                              here ~/a/w/infra
 esc or ← to leave · enter opens it instead
```

**Matches.** `→` on the row draws the strip DIRECTLY UNDER IT and pushes the rest of the
list down by its own height; the frame is exactly as tall as it was, so the composer does
not move. That displacement is the whole argument for bare letters — "the strip pushes the
list down and takes the letters with it" — and a strip under the composer displaced nothing
at all, which is where the first walk found it.

It is drawn this way on every place that has verbs, through the interface and with no place
named anywhere: `place.cursorRow` answers which of the rows just built the cursor is on, and
`placeStripInline` splices the strip in after it. The body is built into `room` minus the
strip's own rows, so the arithmetic is exact; a frame with no room to give does not draw the
strip and does not bind its letters.

Home's ANSWER chips stay at the foot. They are not a row's verbs — they are what a
conversation another window is holding is waiting for — and they belong beside the box that
can answer them.

**One word is not the design's.** The foot says `esc or ← to leave · enter opens it instead`
where the design says `enter opens the chat instead`. The design's sentence is true on home,
on standing and on memory, and false on the tasks place, where `enter` opens the work's own
record; one sentence true everywhere beats a sentence naming a door a key does not open,
which is the defect this whole file exists to end.

## 3d · the time window

**Matches on all three, and the three now draw ONE control.** They answered this question
three ways: standing drew `shift+← aug 25 →` on its header, spend drew a legend that named
the keys and never the span, and the tasks place drew nothing at all while binding all four
keys — which is exactly the defect `verbstrip.go`'s law is written against, and what the
first walk found by pressing `shift+←` and watching the page look wiped. `placeprose.go`'s
`placeHeadRow` is the one head row all three ask for now: what the place is on the left, the
window on the right as the design draws it, and the grain clause beside it where the line
has room.

The tasks place, with its control:

```
 aforge                                                                                                                                                                                      wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

work aforge ran on its own. 7, $2.98 of it.                                                                                                                   shift+← aug 13 – aug 26 →  shift+↑ coarser

done today
› ✓ port the picker                                                                                                             porting the picker 7 files · the picker reads the registry now $1.63 30m

earlier
  ✓ count the tabs                                                                                                                   the tab bar's counts 3 files · each tab wears what changed $0.42 3h
  ✕ the fold spike                                                                                                                                        why the frame jumps gave up, said why $0.05 8d
  · rotate the staging certificate                                                                                                                               the certificate rotation incomplete now
  ✓ move the runner pool                                                                                                              an old dns wobble 4 files · eight runners on the new pool $0.21 1d
  ✓ price the tiers                                                                                                          pricing research 2 files · three tiers, with the middle one marked $0.55 4h
  ✓ the annual toggle                                                                                                                      the annual toggle 1 file · annual is the default now $0.12 1d
        … 25 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 1 done today · 6 earlier
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
 enter go inside it · type to filter · tab next place
```

`shift+←` there pages back a fortnight, and the head line keeps its place:

```
 aforge                                                                                                                                                                                      wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

work aforge ran on its own. nothing.                                                                                                                          shift+← jul 30 – aug 12 →  shift+↑ coarser
        … 37 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
 type to filter · tab next place
```

Spend, with the span between the arrows and the figures on the left — `aug 12 – aug 25` used
to lead BOTH halves of this row, so the label a person moves and the label they read were
two runs of one line:

```
 aforge                                                                                                                                                                                      wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

$3.19 · 810.2k tokens                                                                                                                                         shift+← aug 13 – aug 26 →  shift+↑ coarser
⣦⣦⣦⣶⣶⣶⣷⣷⣷⣷⣿⣿⣿⣿
aug 13                                                                                                                                                                                       today $0.31
aug 26 was the loudest day — $0.31, porting the picker

what ran it · by the model, and the role it was bound to
· deepseek-v4-flash · conversation ████████████ 57 calls · 270.1k                                                                                                                                  $1.08
· claude-opus-4-1 ████████████ 56 calls · 270.1k                                                                                                                                                   $1.06
· claude-sonnet-4-5 ████████████ 55 calls · 270k                                                                                                                                                   $1.04

what it was for
· check the release feed every morning · standing · 45 firings                                                                                                                                     $0.85
· price-the-tiers · pricing-site · a task                                                                                                                                                          $0.82
· porting the picker · aforge-v2 · a conversation                                                                                                                                                  $0.79
▸ 1 more
        … 23 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

`shift+←` on spend pages onto a fortnight nothing was spent in. **Fixed in this lane:** that
frame drew the three sentences saying what the spend place is for — the empty MACHINE's
lesson, over an empty WINDOW — and took the header with them, so the only control that could
page back was off the screen. The place tells the two apart now (`spendPage.held`), exactly
as the tasks place already did:

```
 aforge                                                                                                                                                                                      wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

nothing spent                                                                                                                                                 shift+← jul 30 – aug 12 →  shift+↑ coarser
        … 37 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
 enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place
```

And standing, whose header is the one this helper was generalised from:

```
 aforge                                                                                                                                                                     $1.63 / $20.00 · wed 2:08am
  home   tasks   standing   memory   spend   search   settings
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

  standing orders                                                                                                                                                      shift+← aug 26 →  shift+↑ coarser
  for this project
› ◦ tell me when CI goes red on aforge-v2                                                                                                                              asks first · every twenty minutes
  everywhere
  ◦ always run gofmt before you say a change is done                                                                                                                                               holds
  in other projects
  ◦ check the release feed every morning                                                                                                                    asks first · every morning at nine · last 5m
  ∙ remind me to write the weekly update                                                                                                                                             asks first · paused
        … 30 blank rows …
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                                                                                                                                          here ~/a/w/aforge-v2
 enter open where it was asked · → pause · stop · not here · tab next place · esc
```

**One predicate answers the paint and the keys**, per half: a frame too narrow for the
arrows has no window at all, and one with room for the arrows but not for `shift+↑ coarser`
beside them has no zoom. Standing had that law for its arrows; spend and tasks have it now
too, and the grain half is gated on all three.

**Memory still has no time window**, and FIDELITY item 10 asks for one. There is nothing
behind `shift+←` there to draw, so nothing is drawn and the keys do nothing — a lane of its
own, and `places.md` says so.

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

- **SCREEN 2e, the composer layer, is not built at all** — no dimmed page behind, no lead
  line, none of the three facts, and not its foot. It is a screen's worth of work.
- **2d's `wants your eye` and `gaps it knows it has`** have no mechanism behind them, and
  under the emptiness law a page may not draw the furniture of a feature it does not have.
- **A search hit from an archived conversation carries no project tag**, because the project
  is resolved from the resting list the archive line has left.
- **memory has no time window**, which is the other half of FIDELITY item 10.

## What the SECOND walk fixed, and what it left

Lane FIDELITY-2 closed every row the table above marked **differs**. What changed, screen by
screen, is in each block; what is worth having in one place is the list of things that were
wrong rather than merely absent:

| Where | What it was | What it is |
| --- | --- | --- |
| `place_home.go` `homeCardAnswer` | 1d's answer row drew the chips alone, so `enter open and talk` — the third thing a person can do with a question — was on no surface | the row reads `1 allow once · 2 always · 3 deny · enter open and talk`, the way out dim and the answers amber |
| `homeplaces.go`, `place_standing.go` | an offered place said `a place` and stopped, where 1g's row says what is behind it | `a place · 4 orders, 1 fired today`, taken on home's beat and cached, never on a draw |
| `session/usage_spend.go`, `spendplace.go` | 2c's role column was the word one CALL gave itself, not the crew binding the caption promised; model ids were drawn raw | one row per model, joined against `config.ModelSlots`, headed with the word a person says out loud |
| `place_spend.go` `spendNames` | the `what it was for` join read HOME's cached world, which is dropped the moment home is left — and leaving home is how you get here, so every row wore a raw id | the place reads its own world on the way in and on the beat, and asks every project for its promises |
| `spendplace.go` `spendSubjectRow` | a promise's row said `standing` twice, because its tag already names the kind | the label after the tag is gone |
| `place_spend.go` | an empty spend WINDOW drew the empty MACHINE's lesson and took the header — the only control that pages back — with it; the same defect the first walk fixed on tasks | `spendPage.held` tells them apart; the head row stays and says `nothing spent` |
| `pages.go`, `verbstrip.go` | the verb strip was drawn under the composer, where it displaced nothing — and the displacement is the whole argument for bare letters | the strip is spliced into the body under the row it acts on, and the body is built into the room it leaves |
| `placeprose.go`, `tasksplace.go` | three places with a time window drew it three ways, and one of them drew nothing while binding all four keys | one `placeHeadRow`, one `placeWindowFits` answering the paint and the keys, on all three |

And one row of the first table was not a defect at all: **a place ranks first in home's typed
drop-up and always did.** The column is a drop-up and is read upward; the first walk read it
downward. It is pinned by a test now rather than left to be re-derived.

Left, and flagged to the owner:

- **The `planning · unbound · follows execution` row does not appear on this build.** The
  machinery is there and tested; this window can only be asked about the conversation's own
  model, so it cannot tell an unbound slot from one it has no way to ask about. See 2c.
- **The verb strip's foot says `enter opens it instead`** where the design says `enter opens
  the chat instead`, because on the tasks place `enter` opens a record and not a chat.
- **`standing` and not `promises`** on 1g's margin, for the reason FIDELITY's `tenure`
  deviation gives: one thing gets one noun on this surface.
