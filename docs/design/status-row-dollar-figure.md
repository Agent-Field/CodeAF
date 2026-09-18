# What the chat status row's dollar figure counts under `/task`

**Verdict.** The row's `$` figure is `app.spendShown()` — the larger of the
conversation's own books and `session.UsageTree` over the usage ledger
(`~/.codeaf/v3/usage.jsonl`), summed by the row's `root` field. It counts the
conversation **plus every ledger row whose `root` names it** — which live means
the conversation's own calls and the task's **worker** rounds. It does **not**
count the task's **audit/check round** or the post-landing judge, because those
rows are written with no `root`; they reach the conversation only when the node
closes (the audit) or never (the judge). So while a task is under audit the row
sits flat, and a person reading it — or the different `calls.jsonl` the sessions
watched — is shown a subtree spend the row cannot see.

Diagnosis only. No production code was changed.

---

## 1. The code path that draws the row's dollar figure

The money segment is assembled in `internal/tui3/render.go`'s `telemetry()`, at
the line

```go
add(segCost, costCell(a.spendDrawn()))          // render.go:2677
```

- `costCell` (`render.go:2622`) formats it; `dollars` (`internal/tui3/app.go:9055`)
  spells cents above a cent and four places below it.
- The phone/narrow deck draws the same figure directly: `app.deckSpend`
  (`internal/tui3/statusdeck.go:245-249`) calls `dollars(a.spendShown())`.
- `app.spendDrawn` (`internal/tui3/reveal.go:510`) returns `spendShown()` unless a
  meter-chase is in flight, and a chase only runs while a turn is being written.
  **At rest the row is `spendShown()`.**
- `app.spendShown` (`internal/tui3/treespend.go:133`):

```go
func (a *app) spendShown() float64 {
	if total := a.tree.Folded(); total > a.cost {
		return total
	}
	return a.cost
}
```

  `a.cost` is the **conversation's own books**. `a.tree` is a `session.Receipt`
  set by `app.readTreeSpend` (`treespend.go:57`) from
  `session.UsageTree(lines, self)`.
- `session.UsageTree` (`internal/session/usage_spend.go:721`) sums the ledger
  rows that name this conversation:

```go
case strings.TrimSpace(line.Root) == conversation:    receipt.Children += line.USD
case strings.TrimSpace(line.Session) == conversation: receipt.Direct   += line.USD
```

  `Receipt.Folded()` = `Direct + Children` (`usage_spend.go:700`).

**The ledger it reads** is chosen by `app.treeLines` (`treespend.go:101`): the
host seam when there is one, else the file `app.usageLedger` — the machine's
`~/.codeaf/v3/usage.jsonl` (`session.UsageLedgerPath`, `usage_ledger.go:126`).
**It never reads `calls.jsonl`.**

**When it re-reads.** On the frame clock, and only while the conversation has
work the column can draw — `internal/tui3/app.go:4741-4742`:

```go
if a.railAvail() {
	a.readTreeSpend()
}
```

`railAvail` is `len(a.taskOrder) > 0 || len(a.jobs) > 0` (`task.go:3051`). The
places that ask on purpose (`/cost`, `/status`, the Spending tab, a switch) call
`readTreeSpend` themselves.

**So the row counts**: the conversation's own model calls, plus every
usage-ledger row whose `root` field names the conversation.

---

## 2. Where the task subtree's spend accumulates, and why the two differ

**One row per model call**, written as the call is made, into
`~/.codeaf/v3/usage.jsonl` (`Agent.recordUsageLine`, `internal/session/usage_ledger.go:823`;
`RecordUsage`, `:691`). The row's `root` (`UsageLine.Root`, `usage_ledger.go:224`)
is written from `a.config.rootSession` (`:855`). A task node's agent is built
with `rootSession` equal to its parent's root *or the conversation's own id*
(`Agent.newTaskAgentOn`, `internal/session/task_run.go:7382`, `:7421`), so a
worker's calls carry `root`, and `UsageTree` can add the subtree back onto the
conversation live — no fold required.

**The second ledger is a different instrument.** `~/.codeaf/logs/calls.jsonl`
(`internal/calllog`) is the raw per-call log — one line (or two) per request with
`tag`, `id`, `model`, `cost`, token counts. On this machine it holds 149,879
rows. **It carries no `root` and no `session` at all**, so it cannot be summed by
conversation; it is a flat log whose scope is *every call any door made*. The
status row never opens it. A person comparing the row to `calls.jsonl` is
comparing a tree-summed ledger against a flat call log — two files, two scopes,
two rounding rules.

**The two differ because of a hole in the write side.** A task's audit/check
round is built by `Agent.newAuditAgent` (`internal/session/task_audit.go:3113`)
through `a.newChildAgent(Config{…})` at `:3145`. That `Config` sets
`Workspace`, `Model`, `SessionFile` and `OneModel` — and **neither `rootSession`
nor `usageLedger`**. Compare `newTaskAgentOn`, which does set both
(`task_run.go:7421`). Consequences:

- every audit round's call is written to the ledger with **no `root`**, so
  `UsageTree` never counts it and the live row cannot see it;
- its money reaches the conversation's books only at the node's close, through
  `foldTaskUsage(node, auditor)` (`task_audit.go:1255`; `task_run.go:7168`).

The post-landing **pool judge** is the same shape: `recordPoolUsage`
(`cmd/codeaf/poolrecord.go`) writes `Seat: judge` with no `Session` and no
`Root`, so it is outside both the tree and the folded books.

---

## 3. One real run: the two figures and the gap

Run on 2026-09-18 with the shipped binary
`codeaf v0.2.2-0.20260918041634-81568aa82588` (commit `81568aa8`), a scratch git
repo, an isolated profile:

```sh
codeaf chat --yolo --no-host --model deepseek/deepseek-v4-flash-0731
/task solo create hello.txt containing exactly the word hello
```

Conversation id `07d23d478c5037b3`; task node `1` (journal `d5856d4545878856`);
auditor (checker) journal `a9c535cb29f4e4ee`. The status row was read from the
live terminal; the subtree spend was summed from that run's own
`v3/usage.jsonl`.

| time | status-row `$` (read off the screen) | subtree spent so far (ledger) | gap |
| --- | --- | --- | --- |
| 00:24:48 | `$0.0044` | `$0.0044` | `$0.00` |
| 00:25:03 | `$0.0044` | `$0.0140` | `$0.0096` |
| 00:25:06 | `$0.0044` | `$0.0176` | **`$0.0132`** |
| 00:25:08 (node closes; task reads `done · 39s`) | jumps to `$0.02` | `$0.0176` | — |
| 00:25:43 (end) | `$0.02` | `$0.0237` | — (both draw `$0.02`; see below) |

**The row sat at `$0.0044` for roughly fifteen seconds while the audit round was
spending `$0.0132`** — that is the flat figure the three sessions read as death.
It moved once for the worker (the row showed `$0.0044` at 00:24:48, when the
conversation's own books held only `$0.0001`, so the row was already counting the
task's worker calls through `root`), then stood still through the whole audit,
then jumped to `$0.02` only when the node closed and the audit folded in.

The run's `$0.0237` breaks down as:

| fraction | rows | in the row? |
| --- | --- | --- |
| conversation's own calls | `direct` `$0.0022` | yes |
| task node's worker calls (`root`) | `$0.0043` | yes, live |
| **audit round** (seat `high`, no `root`) | **`$0.0132`** | **only at close** |
| pool judge (seat `judge`, no `session`/`root`) | `$0.0040` | not in the tree; not in the books either (the pool sweep writes only the ledger line) |

At rest the row and the total agree to the display's precision: the books at close
are the conversation's `$0.0022` plus the folded node (`$0.0043` worker +
`$0.0132` audit) = `$0.0197`, and both `$0.0197` and the run's full `$0.0237`
draw as `$0.02`. **The visible gap is the mid-run one — `$0.0132`, the whole
audit round — and it is the only one a person watching a `/task` can act on.**

The header on the same screen told the other story: it read `$0.01 / $500`
during the audit while the row still said `$0.0044`. That figure is the **day's**
spend over the same ledger, through `spendDayTotal` (`internal/tui3/spendplace.go:586`)
read by `readMachineMoney` (`internal/tui3/homemachine.go:107`) and drawn by the
pulse (`internal/tui3/pulse.go`) — every call counted, `root` or not. Two figures
on one frame disagreeing about the same run, because they read different scopes
of the same file.

---

## 4. What the row counts vs what a `/task` reader expects

The row counts the conversation and the task rows that carry a `root` — live, the
worker's calls; the audit/check round and the post-landing judge are missing
until (for the audit) the node closes and (for the judge) never. A person reading
that figure under `/task` — and the header, and `calls.jsonl`, both of which do
move — expects it to be the whole subtree's spend, moving as the work moves.

---

## Appendix — exact file / function map

| what | where |
| --- | --- |
| row's money segment | `internal/tui3/render.go:2677` (`telemetry`), `costCell` `:2622`, `dollars` `internal/tui3/app.go:9055` |
| deck's money segment | `internal/tui3/statusdeck.go:245-249` (`deckSpend`) |
| in-motion figure | `internal/tui3/reveal.go:510` (`spendDrawn`) |
| the figure drawn at rest | `internal/tui3/treespend.go:133` (`spendShown`) |
| the tree read | `internal/tui3/treespend.go:57` (`readTreeSpend`), `:101` (`treeLines`) |
| the one sum | `internal/session/usage_spend.go:721` (`UsageTree`), `:700` (`Receipt.Folded`) |
| frame-clock re-read | `internal/tui3/app.go:4741-4742` (`railAvail`) |
| ledger write / its `root` | `internal/session/usage_ledger.go:823` (`recordUsageLine`), `:855` (`Root`), `:224` (`UsageLine.Root`), `:126` (`UsageLedgerPath`) |
| node's `rootSession` | `internal/session/task_run.go:7382`, `:7421` (`newTaskAgentOn`) |
| **the hole** | `internal/session/task_audit.go:3113` (`newAuditAgent`), Config at `:3145` — no `rootSession`, no `usageLedger` |
| the fold at close | `internal/session/task_run.go:7168` (`foldTaskUsage`); `task_audit.go:1255` |
| the post-landing judge | `cmd/codeaf/poolrecord.go:332` (`recordPoolUsage`) |
| the day figure (header) | `internal/tui3/spendplace.go:586` (`spendDayTotal`), `internal/tui3/homemachine.go:107` (`readMachineMoney`) |
| the other ledger | `internal/calllog`, `~/.codeaf/logs/calls.jsonl` — no `root`, no `session` |
