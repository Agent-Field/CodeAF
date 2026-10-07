# The factory

## /factory — the floor

The **factory** is a place, like home, activity and spend: the floor where work a chat splits
off, or that a connected repository sends, stands in rows grouped by where each item is. Open
it with `/factory`, `alt+9` (`opt+9` on a Mac), or the map (`alt+.`). On a narrow tab bar it
folds into `More`. `esc` goes back to the conversation.

Today nothing is connected, so the page draws one dim line:
`nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected`.
The only key it offers then is `esc back`.

With something on the floor, the left column is the list of items and the right column is the
item under the cursor. `↑` and `↓` (or `ctrl+p` and `ctrl+n`, or the wheel) walk the items;
the hint line says `↑↓ walk · esc back`. On a terminal under 72 columns the list takes the
whole width and the right column is not drawn.

## what a row is

A row on the factory floor is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. It is written as its short name and its title, `#1538 budget caps
per task`; a red CI run is written `ci`.

The rows are grouped under dim headings, in this order, and a group with nothing in it is not
drawn at all:

- `needs you`: an item waiting on your answer, such as a plan to approve.
- `streams`: items running now, and items queued behind them.
- `new`: items that arrived and have not started.
- `landed`: items that finished and wait for your sign-off.
- `shipped`: items you signed off.

The right column shows the item under the cursor, drawn by where it stands: a new item as its
**card**, a running, queued or waiting item as its **stream**, a landed item as its **proof
sheet**, and a shipped item as one line saying when it merged. The page re-reads the floor every three seconds while
you are on it, and the cursor stays on the same item when the floor changes under it. If a
read fails, the floor read before stays on screen and the line above the hint says
`the factory could not be read` with the reason.

## stages, not a pipeline

An item runs through **stages** in a fixed order that a repository or team writes once: for
example plan, write, test, review, neaten, proof. Each stage is one sentence ("read it as a
stranger would"), and it runs as an ordinary task with the ordinary crew picked for that kind of
work. There is no graph to draw and no model to choose per stage; the one word a stage may carry
is its effort, cheap or strong.

A stage can repeat until a condition holds (until green, until clean) up to a number of rounds,
and then it stops and asks you. Nothing inside a stage can add a stage. A stage can also be a
gate: `plan` comes back with the plan before any code, and `ship` waits for your sign-off.

A new item's card lists the stages it would run, numbered, and the stream of a running item
shows them as a strip of phases. A stage can carry a condition (`thin`, `large`,
`touches auth`, `has ui`); on an item it does not fit, the stage is drawn dim with
`· skipped: not thin` (or the condition it missed) after it. You cannot change an item's stages
from the page yet.

## the handover

The **handover** is what happened on the factory floor since you last looked: how many items
shipped, how many arrived, how many asked you something, and what it cost. It is meant to be
the first thing the factory page shows after you have been away.

It is not drawn yet. The factory page today shows the floor's rows and the item under the
cursor, and nothing else.

## the card

A new item on the factory floor is drawn as a **card** in the right column. From the top:

- Its short name and title, and on the right the repository, the kind, the author and how far
  they are trusted (`priya (collaborator)`), how long ago it arrived, and where it came from:
  `github`, `from a chat`, `terminal only`, or `github` with a check when it is on GitHub too.
- Up to two lines of its body, with `…` when there is more.
- The factory's read: one line of facts (`bug · M · tui · ready 72% · ~$3 · risk low`, or for a
  pull request `pr · +218 −44 · 6 files · ci` and its checks), then one sentence of what it makes of
  the item. A thin item adds `thin · it would ask` the author its questions, and
  `nothing posts without you`. An item from a stranger says their words may be drafted on, never
  shipped on, without you.
- The chips: `gate ship [t]   cap $5 [c]   effort — [e]`. The dash means no effort word, so the
  crew picks the effort it would for that kind of work.
- The stages, numbered: `●` for a stage that is on, `○` dim for one switched off, and a skipped
  stage dim with its reason. `×2` is the rounds a stage may take, `until clean` its condition.
- `must show:` and the repository's policy lines.
- `[enter] go` and what the gate means: `runs to a PR · you sign off`, `comes back with the plan
  first`, or `self-ships when the proof is green`. Under it, dim: `[s] add a stage in words ·
  [b] bank · [w] chips in words · [g] also on github · [d] hide`.

None of those keys work yet; see what the factory does not do yet.

## the stream

A running, queued or waiting item is drawn as its **stream**. The first line is a mark (working,
`?` when it waits on you, `○` queued, paused), the item, and on the right the repository, the
bench it is on, how long it has run, and what it has spent over its cap (`$1.42 / $5`).

Under it is the **phase strip**: each stage as a mark and its name, with its round (`1/2`) and how
many tasks it split into (`×3`). The running stage says its minutes left (`review 1/2 · 4m left`);
done stages are muted, pending ones dim, a stage waiting on you in amber, a failed one in red.
Then the recipe in one line (`plan · write · test · review · neaten · proof`), a rule, and the
newest lines of its log that fit, each with its time and a mark for what kind of line it is.

When the item **needs you**, the question is drawn under the log with a `?`, and
`[y] yes · [n] no · [a] answer in words · it waits; the other benches do not`. Those keys do not
answer yet. A **queued** item says `queued · benches full · a bench frees it`.

## the proof sheet

When an item lands, the right column draws its **proof sheet**: `sign-off ·` the item, and
`its claims` (or for a pull request `their claims, from the PR body`). Each claim is one row,
`✓` when it was shown and `✕` when it was not, with the evidence on the right in the claim's own
medium (`test · 0.3s`, a screenshot with `[▦ view]`). A claim nothing showed is listed with
` — not shown` after it, never left out. The repository's policy follows as
`policy · every PR on codeaf must show`, the same way. Then how long it ran, what it cost, and
`diff +218 −44 — the appendix` with `[d] open diff`.

When every row was shown, the last line is `[enter] ship`, then `[c] send back · [o] check
again · [s] the stream`. When any row was not shown, **send back is the default key**:
`[enter] send back — "prove survives a codeaf restart"`, with `[a] ship anyway · [o] check again`
and the line `a failed claim makes the blocking action the default key`.

None of these keys sign off or send back yet. A shipped item shows when it merged, what it cost,
and `[enter] the room`, which does not open anything yet either.

## what the factory does not do yet

Be plain about this when asked:

- **No GitHub, GitLab or Linear is connected.** There is no way to connect a repository yet, so
  no item arrives from one.
- **Nothing posts anywhere.** The factory never comments, labels, opens a pull request or opens
  an issue on any service.
- **There are no verbs.** The page cannot launch, stop, pause, answer, steer, sign off or send
  back an item. The card, the stream and the proof sheet draw their keys (`[enter] go`, `[t]`,
  `[c]`, `[e]`, `[s]`, `[b]`, `[w]`, `[g]`, `[d]`, `[y]`, `[n]`, `[a]`, `[o]`, `[enter] ship`,
  `[enter] send back`) so the layout is settled, but none of them does anything yet. Only `↑`,
  `↓` and `esc` work.
- **The page is empty on an ordinary launch** and draws the `nothing connected yet` line. When
  codeaf is started with the environment variable `CODEAF_FACTORY_FIXTURE=1` the page reads a
  still, made-up floor instead (three repositories, ten items, one in every group) so the page
  can be looked at. That floor never changes and none of its items are real.
- **Over `--host`** the factory page draws the same `nothing connected yet` line.
