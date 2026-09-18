# The work tab: the floor of the software factory

Approved by the owner 2026-09-18. The pitch is a software factory: many runs,
many places, one person steering, not one terminal running one task. This
page rules the `work` tab of the home dashboard, the dot row every run wears,
and the order of the chat's rail. It builds on `TREE.md` (the tree, the fold,
`· N queued behind it`) and on `CHAT-ROLE.md` (a landing speaks only when an
answer is owed). Existing words only: task, step, note, steer, rail, page,
landing card, tab, and the state words work already wears.

## The laws

1. **The tab is called `work`.** The top bar reads `home   work   spend
   settings`. `tasks` named the part; `work` is the word the state words
   already use.
2. **No new nouns.** No project, repo, folder or category anywhere on the
   tab. A run's context is the conversation it came from, dim at the row's
   end: `· poem arc`. The next iteration is one graph of dependencies between
   all chats with auto-classification; this tab is the transition, and the
   conversation tail is the seed of that graph, so nothing is renamed later.
3. **Group by state, order by activity, never a sort control.** Groups in
   this order: `your call`, `running`, `queued`, `done today`. Inside a
   group newest activity first. A group with nothing vanishes.
4. **Typing filters.** No search key: typing on the tab narrows every group
   live, fuzzy over the run's title, its tasks' titles, its brief, its result,
   its notes, and the conversation's title and transcript. The right pane
   follows the top match and shows the line that matched. `esc` clears.
5. **`done` shows today, then folds** to one dim line: `41 more · type to
   find one`. Older work is reached by typing, never by scrolling.
6. **Progress is a dot row, ten cells, always.** Tasks done out of tasks in
   the run. Beside it the fraction is the truth.

## The dot row

| Tasks in the run | A cell means | Example |
| --- | --- | --- |
| 1 | no dots, the state word alone | `running` |
| 2 to 10 | one task | `●●●●◐○○` |
| 11 and up | a tenth of the run | `●●●●●●◐○○○` and `64 of 100` |

Marks: `●` done, `◐` running or a share partly done, `○` not started, `✘` a
share holding a failure. The running cell is the first cell that is not full,
so motion sits at the frontier. Every mark is a slot in the glyph vocabulary
with an ASCII floor (`[######>...]`), resolved through `palette.glyph`.

Width tiers, so the row never wraps:

| Width | Draws |
| --- | --- |
| 90 and wider | `●●●●●●◐○○○  6 of 10 · 2 running · $0.41` |
| 60 to 90 | `●●●●●●◐○○○  6/10` |
| under 60 (the rail) | `●●●◐○  6/10` (five cells, each a fifth) |
| under 40 | `6/10` |

A run that is entirely done reads `done`, never `10 of 10`; one with
failures reads `8 of 10 · 2 failed`.

## The tab

```
 codeaf                                                  $2.41 / $20.00 · fri 12:10pm
 home   work   spend   settings
 ───────────────────────────────────────────────────────────────────────────────────
 4 running · 3 queued · 1 your call · 2 done today                        by tree ▾

 your call · 1                             │ rewrite the auth flow   ●●●●●●◐○○○  6 of 10
 ? migrate the ledger      ○○○○  · ledger  │ from auth rewrite · running 18m · $0.41
 running · 3                               │ work glm-5.3-flash · plan glm-5.3
 ▶ rewrite the auth flow ●●●●●●◐○○○ · auth │
 ▶ index the poems       ●●◐○   · poem arc │ your call
 ▶ the 6am repo watch           · watch    │  ? keep the old table for a week?  1 yes 2 no
 queued · 1                                │
 ○ nightly bench         ○○○    · bench    │  ▶ write the handler    12 steps · 2 queued behind it
 done today · 2                            │      $ go test ./internal/auth/...
 ✔ five chapter poem     ●●●    · poem arc │  ▶ write the middleware 4 steps
 ✘ index the poems, 1st  ●✘     · poem arc │  ○ write the tests      waits: write the handler
   41 more · type to find one              │    └ ○ write the fixtures  waits: write the tests
                                           │  ✔ 3 done                                    ▸
                                           │ notes
                                           │  › a note reaches the worker at its next step
 ───────────────────────────────────────────────────────────────────────────────────
  ↑↓ move · enter open · tab by state · n note · p pause · c cancel · esc clear
```

- **Counts strip**: the state counts and the reading toggle. Counts drop at
  zero.
- **Left**: one row per run: mark, title, dot row, conversation tail.
- **Right, the selected run**, in the order a person can act: the header
  with dots, the conversation, the seats (`work <model> · plan <model>`);
  `your call` first, every open question of the run with its answer keys;
  then the tree as `TREE.md` draws it, each row's seat dim at its end;
  `notes` at the foot. `enter` on a row drills into that task's page, `esc`
  returns. `tab` flips the tree to the by-state reading (running, queued,
  done, one column).
- **Steering keys** act on whatever is selected on either side. A note here
  and "add to <task>" said in the main chat write the same note row.

Filtering, with `middlew` typed:

```
 › middlew|
 running · 1                                │ rewrite the auth flow  ●●●●●●◐○○○ 6 of 10
 ▶ rewrite the auth flow ●●●●●●◐○○○ · auth  │ note · you: keep the middleware order
 done · 1                                   │  ▶ write the middleware   4 steps
 ✔ api middleware, first pass ●●● · auth    │      $ cat > internal/auth/mw.go <<'EOF'
```

## The rail (the chat screen)

The rail is the left column of the tab without the groups' headings: running
families first, newest activity on top, then queued, then done folded with
age. Inside a family the tree keeps store order. Running rows float to the
top of their family and its done rows fold to one `✔ N done` line at the
bottom, so a ten-task run never pushes the live rows off screen. A run's
row wears the five-cell dot row.

```
tasks
▾ ▶ rewrite the auth flow  ●●●◐○
  ├ ▶ write the handler        $ go test ./internal/auth/...
  ├ ▶ write the middleware
  ├ ○ write the tests          waits: write the handler
  └ ✔ 3 done
▸ ○ migrate the ledger      ○○○○  waits: rewrite the auth flow
▸ ✔ five chapter poem       12m
```

## What the surface needs

- Per run: tasks done, running, queued, failed over the subtree; the
  conversation title (`Task.Chat` resolves to it); the seats; the open
  questions (the run's held tasks and their questions).
- Fuzzy search over runs and their conversations: the home's reach search
  is the one door; the tab feeds it the run's rows as well.
- Nothing new in `internal/plandb`.

## Parked for the next iteration

A view by sentence: "all work related to marketing" as a saved view, a
classifier over old and new runs, corrected from the chat ("this one is
marketing"). It becomes the graph of conversations.

## The cells

1. **dots and order**: the dot row with its tiers through the glyph
   vocabulary; the run's counts on `PlanTaskRow`; the rail's activity order
   and the in-family fold of done rows. Failing test first.
2. **the work tab**: rename, the counts strip, the left groups with the
   conversation tail, the right pane with `your call` and seats, `tab` for
   by state, typing to filter through the reach search, `done today` fold.
   After cell 1 and after the tree cell (c246).
