# Asking codeaf to do something

There is no syntax. You write what you want the way you would write it to a
colleague, and the front desk decides where it goes.

> "Draft the launch note and save it as launch.md."
> "Find out which of our dependencies changed licence this year."
> "Read this contract and tell me what I'm agreeing to."

## Reflex or compiled

Two shapes of work exist, and codeaf chooses between them for you.

A **reflex** is one obvious, reversible, seconds-scale action. It skips
compilation, planning and delivery review, gets four turns and a **90-second**
deadline, and still journals like anything else. It is not a synonym for
"lookup" — it is a licence granted by reversibility, not by size.

Anything else is **compiled**. Your verbatim instruction goes to the intent
compiler, which turns it into a brief: the goal, the concrete deliverable, a
budget, the shape of the work, and every default it had to supply. Then it is
planned and worked.

Compiling and planning are single model calls, each with **four minutes** (or
its share of what the request has left, when that is less), and a model that
thinks before it answers is told how much thinking fits in that time on the
machine serving it. It is told nothing on a machine whose speed is not yet
known, or by a provider that has refused a thinking budget; it then thinks as
it would by default, under the same wall. One that is still thinking when its
time runs out — at that wall or at any shorter bound the run puts on it — is
asked once more, with what it had worked out in front of it, for the answer
that work reached. If a request runs out of time twice anyway,
it is not started and you are told: `I couldn't get this planned — the model
thought past its time twice. Say 'try again' to requeue it.`

Anything irreversible is always compiled, however small it looks: spending or
transferring money, publishing, sending, deploying, or deleting outside the
workspace.

## The compile receipt: assumptions you can revise

codeaf does not stall your work with a questionnaire. It assumes sensible
defaults — scope, audience, format, quality bar, evidence, timing — and then
declares every one of them back to you. That list is a receipt, not a decision
you are stuck with. Read it, and if one is wrong, say so and the plan is edited.

## When it does ask

It asks in exactly two situations, and it asks once:

1. **The gap is expensive and hard to reverse** — real money leaving, something
   existing being overwritten, something being sent on your behalf, or a guess
   that would waste most of the budget. It states its best guess so you can just
   say yes.
2. **It cannot tell what you meant** — your message points back at earlier work
   and more than one job plausibly matches. It lists the candidates in your own
   words and numbers them. One plausible match is not ambiguity; it proceeds.

Questions arrive as cards you can answer with `1`–`9`, arrows, or free text.

Over time it asks less: once you have accepted a category's default enough
times, codeaf stops asking it and simply declares the assumption instead. The
questions that carry consent — ratifying a standing goal, raising your budget,
keeping a server alive — are never silenced this way.

## Attachments, images, documents

Drag a file path into the input to attach it. Attaching copies the file: the
bytes are kept where codeaf lives, so a retry next week and a follow-up
tomorrow read exactly what you attached even after you have moved, renamed, or
deleted your own copy. Your file is read once and never written to.

- **Images**: if your current talk model can see, the image goes to it directly.
  If it cannot, a vision model looks on its behalf and the answer comes back
  labelled `seen by <model>: …` so you always know who looked. Either way the
  image is staged into the job's workspace, so a worker can look at it too —
  and when nothing available can see an image, the answer says so rather than
  quietly working around it.
- **Documents** (`.pdf`, `.docx`, `.pptx`) are read with a cost ladder, cheapest
  rung first: local extraction if the tools are on your machine (free), then a
  free remote parse, and only for a genuinely scanned document does it escalate
  to OCR — which is priced at about **$2 per 1,000 pages** and passes through
  your daily rail before it spends. Repeat reads of the same file are cached
  next to it, so you pay at most once. Two limits are real: the local and OCR
  rungs are PDF-only, so `.docx` and `.pptx` are read remotely, and asking for
  a page range only means something for a PDF.

## Attribution: how codeaf signs git work

Every commit codeaf writes for you — a worker's own, and the one the harness
writes when work lands — ends the message with one blank line and the same two
trailer lines, in this order and nothing after them:

    Assisted-by: CodeAF (deepseek-v4-flash)
    Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>

The name in brackets is the model alone, without the provider or company in
front of it and without a routing suffix like `:free`. Nothing goes in the
subject or the body. When it
opens a pull request or an issue, it ends the body with an
em-dash line and one sentence: *Drafted with CodeAF · reviewed and owned
by the author*, linking to `agentfield.ai/github/codeaf`.

That is the whole of it. It is provenance, not a byline: never inside a code
file, never in a commit subject, never in your README, and never in a
deliverable like a deck or a report. A repository that forbids AI trailers wins
— its CONTRIBUTING or policy is honoured and the worker tells you it left the
signature out.

It cannot be turned off: the `attribution` setting and `CODEAF_ATTRIBUTION`
are gone, and a profile still holding either is told so when codeaf starts. The
one part you can turn off is the model's name — the **model in commits** row
(`attribution.model`) under **sharing**, or `CODEAF_ATTRIBUTION_MODEL` from your
shell — and then the first line is just `Assisted-by: CodeAF`.

## Where a coding worker actually works, and what happens to work that was not brought back

A coding worker that shares a directory with other workers, or that is pointed at
a repository of yours, is given a **checkout of its own** — a git worktree under
codeaf's own state, on a branch named `codeaf/leaf/<the part's id>`. It edits
there, runs the project's own checks there against a tree no sibling can move
under it, and when it succeeds its branch is squashed back into your workspace as
**one commit** with a real message. Your directory is never worked in directly.

**Work that does not come back is never thrown away, and it is never silent.**
When the work is refused — a check said it is not right, or its change would not
merge — nothing is applied, because a change nobody has judged good must not be
put into a tree other work is being checked against. What you get instead is one
sentence, on the result and in the running output, saying where it is:

```
This work is written and it is not in your workspace: 4 file(s) are in <the checkout>, on branch codeaf/leaf/<id> — `git merge codeaf/leaf/<id>` brings it over.
```

The checkout and the branch are both kept for **seven days**, so the sentence is
still true when you come back to it on Monday. Whether that work belongs in your
repository is a decision for you and not for the run.

That sentence is written from **git**, never from what the worker said about
itself: codeaf reads the checkout's own status and the commits on its branch. A
worker that reports having written everything and a worker that reports having
written nothing are equally unreliable about it.

## What the reply promises

A receipt says the work is queued and that you will be told when it lands. It
never says the work is finished, never gives you a time, and if something is
already running it says the new work is queued behind it.

If something genuinely cannot be done as literally asked, you never get a bare
"no": you get the nearest thing that *can* be done, offered as the default it
will proceed with, or as a small numbered choice.

## What the work found

Ask what a job found, produced or concluded and you get the finding, not its
status. "It completed" is not an answer to that question.

Two mechanisms make that true. When your message clearly points at particular
jobs, their full results ride into the answer alongside the one-line board — the
summary whole, the files they wrote, what they spent, when they finished — in a
budget of their own, so depth never crowds out breadth. And the front desk can
read any single job's whole result on demand, including what each of its parts
concluded, when it needs more than arrived with the question.

Which jobs get opened is decided by how well your words match the work itself,
never by a list of phrases. A greeting or a fresh request opens nothing, and a
result you were shown a moment ago is not repeated back at you.

## What the review reads before work is handed to you — the files, not the write-up

When a job changed files, the review that decides whether it is done reads **the
change itself**: every file the work wrote or changed, with what is in them. The
worker's own write-up sits beside it, marked as what the worker *says* about the
work. It is never what is judged.

That is deliberate, and it is a fail-safe. A write-up that is thin, oddly
worded, or that comes back as a lump of data instead of sentences cannot make
correct work look unfinished; a confident write-up cannot make missing work look
finished. One measured run lost twenty-eight minutes and 46 cents to exactly
that — the worker's last message came back as a data structure, and three
reviews in a row said the work "contains only text describing what was
supposedly done" while six changed files sat on disk.

The files are shown to the review **whole**, or named and not shown at all —
never part of one. A reviewer handed the first two kilobytes of a long file
reports the file as truncated, which is true of what it was handed and false of
what you get; three refusals across two runs were exactly that.

So a finding about a job that changed files always names **one of those files**
and quotes **one of the behaviours your request states** that the file falls
short of, and that is how you read it:

```
gate: fail — src/textual/widgets/_rich_log.py — follow_end is declared and never posts FollowChanged
```

A review that can name neither a file nor a behaviour you asked for is not an
answer at all. It is asked again, and if it still says nothing the run reports
itself **short** rather than finished — a check that did not happen is never a
check that passed. "The file is truncated", "the deliverable does not contain
the actual content" and the like are not findings: they are about what the
review was shown, and the files are on disk either way.

And when the review says the work is short while the job's own reading of what
it is judged on says nothing is left to do, those two cannot both stand. The
first round the review buys is never refused on that ground; and if the reading
still finds nothing to add, the **review** is what was wrong — it is recorded as
overturned and the work is handed over as finished, rather than left as a
shortfall you cannot act on.

Where a job produced no files — a question answered in words — the message *is*
the answer, and the review reads the message, exactly as it always did. A file
your request named and nothing wrote is still named back to you as missing.

And a write-up that arrives as a lump of data is asked once, before anything
else, for the answer in plain words. That ask is written down against the job,
so a run that had to ask twice never looks like one that asked once.
