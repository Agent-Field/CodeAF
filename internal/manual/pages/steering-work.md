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
lands." There is no cue list any of these has to match. Every message you send
reaches the front desk's own hands, which are a fixed set of typed tools over
the live board: it reads the board, then acts against ids it actually saw
there. It cannot invent a target, and every safety rule above — the confirm
gates, the state rules — still governs it. A misread sentence therefore costs
you one question, never a silent wrong action.

## What the front desk's hands actually are

Reading is always safe and never needs permission: the board, one job's result,
its plan, a file it wrote, the manual, what has been spent, what is on watch,
what was finished in a window of time, and a search across everything
remembered. Reads never queue anything.

Three of those reads are total, and between them nothing the brain knows is out
of reach from the conversation:

- **recall** — one search over everything settled or said at once: what you
  said, what you told it, what it did, the standing rules, the services. Each
  hit comes back with real content and an id, and it can be narrowed to one
  kind of thing, to a window of time, or to one of your chats.
- **open** — one thing whole. Work still running opens as its plan with every
  step's state, what its workers are saying right now, the files it has written
  so far and what it has spent; work that finished opens as its whole result,
  its files, its spend and how each part ended; a file opens as its actual
  bytes; a standing rule, a service or a numbered notebook line opens as its
  full record. Asked for it raw, it hands back the journal's own rows —
  every event and every message attached to that job, unedited. Nothing here is
  truncated: anything longer than a page is *paged*, and the reply says which
  part of how many it is holding, so a fragment is never mistaken for the whole.
- **status** — the whole system on one page: what is running, queued and
  failed, what today has cost against your daily limit, what is on watch and
  when each check is next, what is running as a service, and what has been
  measured about how the work goes.

One read is about conversations rather than about work: **thread** opens another
of your chats and hands back the end of it. It is only ever read when the front
desk needs it — nothing about another room is carried around in the background —
and the chat you are in is already in front of it, so it never re-reads that one.

The rest change something and each one journals:

- **task** — commission one piece of work. This is how anything gets started,
  and it carries your words verbatim. One ask is one task: the workforce breaks
  it up, the conversation never does. It has four things that can travel beside
  the ask — the conversation itself, when the requirements live in what you have
  been discussing rather than in one sentence; the finished job it amends, when
  you are rejecting something already delivered and want the previous version to
  go back in with your criticism; the work it follows on from; and the model you
  named for it.
- **change** — hand your own words, verbatim, to something already under way or
  already standing: a live job, a standing rule, a service, a way of working.
  It carries no verb at all. What your words MEAN for the work — tell the people
  already working, edit the remaining plan, both, or bring it forward — is
  decided by the side that can see the plan, and what it came to is said back to
  you when it lands.
- **stop** — withdraw things: cancel live work, retire a standing rule or a way
  of working, stop a service. Say pause or hold and the two kinds that can be
  held are held instead of ended. "Everything" is the total one, and it asks
  once about live work before touching it.
- **write** — put a document on disk and hand you the path. Anything you will
  use outside the conversation — a diagram, a document, code, data — is born as
  a file rather than typed into a reply.
- **note** and **forget** — write one durable thing into what aforge has
  learned, or let one numbered line go when you say it is no longer true.
- **ask** — one short numbered question when more than one thing plausibly
  matches what you meant. It never picks for you.
- **answer_question** — settle a question a worker is blocked on. Aforge may
  only do this for questions explicitly marked informational; anything that
  needs your consent comes to you and stays open until you answer it.
- **interrupt** — stop the turn the front desk is in the middle of. That is
  different from cancelling work on the board, and it is deliberately not the
  same verb.

The front desk does all of that inside one reply. It can read the board, decide,
commission work, and report back without handing you off to anything.

## Receipts count and name things

Every change comes back as a receipt that counts what moved and names it in your
words: "Cancelling 4: Line scans, Market research…". A receipt states what was
queued, never what was delivered, and never promises a time.
