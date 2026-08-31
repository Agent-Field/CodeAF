# The division judge's "no worker can do this" is bought, read, and thrown away

Investigated on `fix/divide-human-only`, cut from `origin/dev` at `74695a92`.
**No code changed.** The finding below can be expressed entirely on the division
side, but the landing it needs cannot be reached from any file that is not
frozen under the #77 / `feat/task-ground` rewrite. Per the lane's own standing
order, nothing was forced.

---

## 1. What was measured

Journal: `~/.aforge/v3/projects/-Users-santoshkumar/8f75d6c1ffdeaaf5/tasks/20260831-115001_7.jsonl`,
second and fourth lines. Task 7's brief was `(A | B) > C` — A = approve PR #1018,
B = approve PR #1019, C = the merge queue merging both by itself.

The division record, verbatim:

```json
{"type":"division","division":{"taskId":7,"source":"sketch","requested":2,
 "decision":"refused:review",
 "error":"refused: Nothing here can actually be divided or done by a worker: A and B are the same single action — an approving review GitHub will only accept from a human who isn't the author — and C is just the merge queue and CodeQL re-scan happening on their own afterward. The one real job left is to escalate the approval blocker to the user and then verify the merge and alert closure, and that is one pair of hands, not three.",
 "parts":["approve PR #1018","approve PR #1019"]}}
```

That answer cost $0.0205 and arrived at 15:50:28Z, **before the worker had made a
single request**. It is a correct and complete finding about the work: no worker
can do what is left, because GitHub will not accept an approving review from the
author and the rest happens by itself.

What happened next, from the same journals:

| journal | what it is | spend |
| --- | --- | --- |
| `…115001_7.jsonl` | the worker, run anyway | $0.2664 |
| `…115204_7-audit-83c94b.jsonl` | the check | $0.3634 |
| `…115452_7-repair1.jsonl` | the repair round | $0.5594 |
| `…115800_7-audit-dc8320.jsonl` | the check again | $0.0550 |
| | **total** | **$1.2442** |

Eight minutes wall clock (15:50:01Z → 15:58:56Z), a worktree, a repair round and
two checks, spent on work the judge had already said nobody could do — and the
node still landed failed. The person was shown a failure where the honest answer
had been bought nine minutes earlier.

---

## 2. Where `decision=refused:review` lands today, precisely

The whole road is `internal/session/task_divide.go`.

1. `Agent.divideOnce` (`task_divide.go:582`) is the one body both askers reach. It
   opens one `journalDivision` line and writes it on every road out with a defer
   (`:584-585`).
2. `Agent.reviewDivision` (`task_divide.go:953`) makes the one mastermind call. A
   reviewer answering `{"refuse":true,"why":"…"}` returns
   `nil, divisionNotAsWritten(review.Why), "refused: " + why` (`:1004`).
3. Back in `divideOnce` (`:702-712`): `line.Decision = divisionRefusedReview`,
   `line.Error = why`, and **the refusal sentence is returned to the asker**. That
   is the entire consequence. The finding leaves the building as a string.

There are exactly two askers, and only one of them is the measured case:

- **`divide_work`**, the worker's own verb (`task_divide.go:568`). The refusal is
  an ordinary tool result and the worker carries on — which is right, because
  that worker is already running and nothing on this road can unspend it.
- **the sketch**, `Agent.divideFromSketch` (`task_divide_sketch.go:113`), which is
  the measured case (`"source":"sketch"`). It calls `divideOnce`, then asks **the
  graph** whether children exist — deliberately never the sentence — finds none,
  and returns `""`. That empty string is the same answer as "the road is off",
  "nothing was drawn", "the floor refused", "no lane was free" and "the reviewer
  refused". Its one caller is `Agent.workTaskNode` at `task_run.go:2812`, which
  reads it as a brief prefix and then runs the worker at `:2814`.

So: the judge's finding is journalled and discarded, and the empty string it
becomes cannot say anything else.

---

## 3. The two cases share one word, and today nothing can tell them apart

- **(a) the ordinary refusal** — this is one job, or these parts overlap, and a
  single worker can still do the work. It must keep running exactly as it does
  today. This is the overwhelming majority of `refused:review`.
- **(b) the human-only finding** — nothing that is left can be done by any
  worker at all. The measured case.

Nothing in the record distinguishes them:

- `divideReview` (`task_divide.go:927`) is `Refuse bool`, `Why string`,
  `Parts []dividePart`. There is no third answer.
- `journalDivision` (`sessionfile.go:524`) is `Decision` and `Error`, and
  `Error` is the model's free prose.

The only carrier of (b) today is the **free text of `why`**. Reading it — a
substring, a keyword, a phrase list — is exactly the false positive this lane was
told to write the test against first: a conservative reviewer's "this doesn't
divide well" and a genuine "no worker can do this" are one prose sentence apart,
and a task parked on a person because a model chose a discouraging word is worse
than the bug being fixed. **(b) is not expressible today and cannot be recovered
by reading.** It has to be asserted.

---

## 4. What the division side would need — all reachable, no frozen file

Every item here is in `task_divide.go`, `task_divide_sketch.go` and
`prompts/divide.md`. None of it touches `task_contract.go`.

1. **`divideReview` gains one explicit field**, e.g.
   `Nobody bool` with the wire name `nobody` — an assertion the reviewer makes, never an
   inference from its prose. That is the whole answer to the false-positive law:
   the reviewer has to reach for the word.
2. **`divideReviewBrief` (`task_divide.go:880`) teaches the third answer**, one
   paragraph under the existing REFUSE paragraph, written narrow:

   > `{"refuse": true, "nobody": true, "why": "…"}` — and `nobody` ONLY where what
   > is left cannot be done by a worker at all: an approval only a person may
   > give, a credential nobody here holds, a decision that is the person's to
   > make. It is NOT the answer to "this is one job" or "this does not divide
   > well" — those are a plain refusal and the worker carries on.

   The same paragraph belongs in `prompts/divide.md`, whose "TWO THINGS DECIDE"
   and "AND THEN THE PLAN ITSELF IS READ" sections currently promise the worker
   that a no always means "carry on with the work in your own hands".
3. **A fourth decision word** beside the three at `task_divide.go:540-555`, e.g.
   `divisionRefusedNobody = "refused:nobody"`. The file's own stated law is that
   the gate is named in the refusal so a record can tell a road that is working
   from a road that is switched off; a task parked on a person is a fifth
   distinct finding and must not be spelled as the fourth.
4. **`reviewDivision` carries the flag out** and `divideOnce` journals the new
   word, with a new sentence in the `not split:` family beside
   `divisionTooNarrow` / `divisionNoLane` / `divisionNotAsWritten`
   (`task_divide.go:1130+`) — one that does NOT end "carry on with the work in
   your own hands", because that instruction is what produced the empty-tree fix.
5. **`divideFromSketch` returns a second value** — the judge's own sentence — set
   only when the flag came back and no parts were admitted.

Steps 1–5 land cleanly and change **nothing about what runs**. Without step 6 the
node still spends its worker; all that improves is the record.

---

## 5. The seam that cannot be reached, and why

The landing is the whole fix, and it is inside the frozen file. The chain:

- The only call to `TaskGraph.complete` in the package is `task_run.go:2525`, in
  `Agent.runTaskNode`. (`grep -n "\.complete(" internal/session/*.go` — one hit
  outside tests.)
- The state it passes is whatever `work(ctx, node, listed)` returned, and for a
  worker node that is `Agent.workTaskNode` (`task_run.go:2689`).
- **Every** unverified landing in the package is a `return TaskUnverified` inside
  `workTaskNode` or its three helpers `landStopped` / `landShifted` /
  `landConflicted` (`task_run.go:3074`, `:3131`, `:3159`). All four are in
  `task_run.go`.
- A divide-side landing cannot be smuggled in beside it. `TaskNode.finish`
  (`task_run.go:1484`) overwrites the report unconditionally, so `workTaskNode`'s
  own landing would write over the judge's sentence; and `complete` closes
  `node.done`, so a second one panics.
- Nor can the run be diverted. Cancelling from the divide side reaches
  `case ctx.Err() != nil` → `"paused — it resumes"` and `return ""`, which is
  recovery's road and not a landing. Marking the node stopped
  (`TaskGraph.stop`, `cancel.go:133`, not frozen) reaches `TaskFailed` with
  `"stopped"` as the whole report — the wrong word, and the finding lost.

**Conclusion: no file outside the frozen set can make a running node land needing
a person.**

### The exact seam needed — four lines in `task_run.go`

`task_run.go:2810-2813` today:

```go
		if handedOut == "" {
			handedOut = child.divideFromSketch(ctx)
		}
```

becomes:

```go
		if handedOut == "" {
			var person string
			if handedOut, person = child.divideFromSketch(ctx); person != "" {
				return a.landNeedsPerson(node, tree, person, log)
			}
		}
```

with the landing itself written on the divide side, in
`task_divide_sketch.go`, as `landShifted`'s twin (`task_run.go:3131`):

```go
func (a *Agent) landNeedsPerson(node *TaskNode, tree taskTree, why string, log io.Writer) TaskState {
	merge, kept := keptWork(tree, node.title(), nil)
	fmt.Fprintf(log, "no worker was started: %s\n", why)
	node.finish(needsLookLead+why, kept, tree.branch, merge)
	return TaskUnverified
}
```

Everything downstream already exists and nothing else has to learn about this:
`needsLookLead` (`task_audit.go:351`) is the register; `TaskUnverified` is the
state the settle card, the rail's `?`, the landing note's "needs your look" verb
and `bubbleUnverifiedChildren` (`task_run.go:2553`) already read; `keptWork` on an
untouched tree commits nothing and releases the worktree keeping its branch.

**The position is load-bearing.** It must sit after the worktree and the child
agent — both already paid for, and both cheap — and before `runTaskChild` at
`task_run.go:2814`, which is where the eight minutes and the $1.22 are.

One wording decision is left for whoever lands it: `needsLookLead` reads
"finished, but needs your look — " and this node never started. The recommendation
is to keep it anyway — one vocabulary, and `taskOutcome` reads the first line
straight onto the card — rather than mint a second lead that says the same thing
in different words.

---

## 6. The tests, conservative case first

1. **The conservative refusal must not change.** A scripted reviewer answering
   `{"refuse":true,"why":"parts 2 and 3 edit the same file"}` — no `nobody` — must
   leave `divideFromSketch`'s second value empty, journal `refused:review` exactly
   as today, and let the worker run. Write this one first; it is the whole
   false-positive law.
2. **The measured shape lands on the person.** A reviewer answering
   `{"refuse":true,"nobody":true,"why":"<the sentence quoted in §1>"}` admits no
   parts, journals `refused:nobody`, and hands back the sentence — with a run-side
   assertion that no worker request was made and no worker spend recorded.
3. **The report is readable and clean.** The person-facing report carries the
   judge's own reason, sits under `needsLookLead`, and contains none of
   `auditor`, `verdict`, `verified`, `refuted` — the ban that
   `task_landing_test.go:252` already enforces for the other landings.

The fixtures for 1 and 2 exist: `task_divide_sketch_test.go`'s `nest` harness and
the scripted reviewer used across `task_divide_test.go`.

---

## 7. Obligations the landing wave inherits

- **The manual law.** A new refusal wording and a new way for a task to reach
  "needs your look" is a feature change, so `internal/manual/chat/` changes in the
  same commit. Grep the corpus for `not split`, `divide`, and any page that says a
  task only lands needing a look after a check has run — that claim becomes false.
- **`prompts/divide.md`** currently promises the worker that every no means "carry
  on with the work in your own hands". The (b) answer contradicts it and the page
  has to say so.
- **Refusal wordings on `origin/feat/task-ground`.** `task_divide.go` is byte
  identical there (`git diff origin/dev...origin/feat/task-ground --
  internal/session/task_divide.go` is empty), so the divide side is uncontended.
  `task_run.go` is +382 lines on that branch, which is exactly why it is frozen.

---

## 8. Recommendation

The division-side half (§4) and the four-line seam (§5) are one change and should
land as one, because §4 alone improves only the journal while the money keeps
being spent. The cheapest honest route is to hand the four lines to whoever owns
the `feat/task-ground` / #77 rewrite of `task_run.go` and land the rest of this
lane on top of it, or to unfreeze `task_run.go` for a single commit.

There is one improvement reachable today with no frozen file, deliberately not
made here because this lane's orders were to change nothing if the landing could
not be routed: on the **`divide_work`** path the (b) answer should tell the worker
to stop and report that only a person can do what is left, instead of today's
"carry on with the work in your own hands" — the instruction that, on the
measured cell, produced a fix to a file in an empty tree.
