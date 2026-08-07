# Steering work that is already underway

Once something is running you are not stuck with it. You can ask about it, stop
it, redirect it, or push it along — all in the same plain sentences, with no
ids and no commands to memorize.

## Asking is never doing

"What's running?", "how far along is the audit?", "what did that cost?" are
reads. They never change anything and they never queue anything. A status
question earns an answer off the live board, never a verb.

## Stop, pause, restart, reorder

Name the thing in your own words and say what you want:

- "cancel the finance job"
- "pause the scans for now"
- "resume the research"
- "restart the one that failed"
- "do the API audit first"

Cancel and pause apply to work that is queued or running; resume applies to
paused work; restart applies to failed or cancelled work; reordering — aforge
calls it **reprioritize** — applies to work that has not started. If you ask for a verb the thing's state cannot take,
you are told which state it is actually in rather than being silently ignored.

## Sets: "cancel the queued ones"

A plural marker turns a status word into a set. "The queued ones", "all the
failed ones", "stop them all" resolve against the board as a group — not as one
fuzzy guess. "The failed one", singular, still means the one thing you have in
mind.

"Everything" is the only word that reaches past your own work into aforge's own
practice and charter internals. "All", "the rest", "them all" mean all of *your*
work.

## Why a big cancel asks you to confirm

Some changes are worth one question, and aforge asks it exactly once, before
anything moves. A confirm appears when:

- the change would reach **more than 3** tasks — this applies to any change,
  including pause and resume; or, for **cancel and restart** only,
- the work already spent **more than $0.25**, or
- it has been running for **more than 5 minutes**.

The question names the count and the loss — "Cancel 6 tasks? 11 minutes in and
~$0.42 spent." — because the difference between one task and six is the whole
reason you would want to be asked. Until you answer, nothing has changed. If you
say keep, nothing changes at all. Small, cheap, quick things are simply done, no
question asked. That is why some cancels ask and others do not.

## "Not just X, I want Y"

When what the work is *for* has changed, say so in your own words. Your sentence
is handed to the running job verbatim and its remaining plan is edited to match
it — no new job queued behind the old one.

> "not just the summary, I want the raw numbers too"

The immediate receipt promises only the handoff. What actually changed in the
plan is written a moment later by the part that did it, because that is the only
place that honestly knows.

This is a revision — aforge calls it an **amend** internally — not a new job.
Your original words stay the authority; only the remaining plan moves.

## Steering one worker mid-turn

You can also add a constraint without changing the plan: open a task and type
into it, and the line lands before that worker's next turn. Nothing is
re-planned; the person doing the thing simply hears you.

## "Complete it fast"

Urgency is its own verb — aforge calls it **expedite**. "asap", "hurry up",
"just finish it", "give me what you have" do not queue anything new; that would
make the wait longer. Instead the job you named moves up the claim order and its
unstarted tail is trimmed to the shortest path to the deliverable. Expedite
never adds work, and it never asks: everything it touches is cheap and
reversible.

Aforge will never say it will hurry something unless it actually did. If you say
"make the parser faster", that is a different deliverable, not a schedule — it
is heard as work, not impatience.

## Sentences no rule anticipated

"Kill everything except the finance one." "Hold the scans until the research
lands." For these, aforge reads the live board and then acts through a small
fixed set of verbs against ids it actually saw there. It cannot invent a target,
and every safety rule above — the confirm gates, the state rules — still governs
it. A misread sentence therefore costs you one question, never a silent wrong
action.

## Receipts count and name things

Every change comes back as a receipt that counts what moved and names it in your
words: "Cancelling 4: Line scans, Market research…". A receipt states what was
queued, never what was delivered, and never promises a time.
