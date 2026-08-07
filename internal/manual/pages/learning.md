# What aforge learns, and how to correct it

## The notebook

Everything durable aforge believes lives in one scoped notebook. Open it with
`/notebook` (or `/memory`), or `aforge notebook` from a shell.

Each belief has a **kind**:

- `preference` — how you like things done
- `fact` — an entity and its stable attributes
- `quirk` — an oddity of one file, repo, tool, or product
- `lesson` — a mistake turned into a rule
- `skill` — a procedure that was actually executed and verified
- `playbook` — one scoped strategy bullet earned from earlier work
- `unsettled` — two competing approaches, not yet decided
- `question` — a known gap in what it knows
- `trait` — a measured second-order fact about how you work

And a **scope**, so a belief about one repo never leaks into another:
`user`, `env`, `tool:git`, `repo:/path`, `file:/path/file`, `domain:pricing`.

## Moments in the thread

You see learning happen as quiet lines, not as a report:

- `· learned — <the belief>` — something durable was recorded
- `⚒ forged: <name> — proved twice, now available` — a procedure worked twice
  and became a real, reusable skill
- `⚖ settled: <claim> · 4 trials` — an unsettled pair was decided by actually
  running both
- `· let go — <the belief>` — a belief left the notebook
- `· reflected — 2 beliefs learned, 1 belief aged` — the retrospective's summary

When several land at once they collapse into `· learned 3 things ▸`, which you
can expand.

## "reflected — N beliefs merged"

*Merged* means consolidation ran. When one scope gets crowded — around a dozen
beliefs — aforge rewrites them into fewer, sharper ones and marks the originals
**superseded**. Superseded beliefs stop being retrieved; they are not deleted,
and the journal still shows what they were and what replaced them.

*Aged* is different: those beliefs were not replaced, they simply stopped being
used. See below.

## Credibility: not all beliefs are equal

Aforge tracks where each belief came from — you said it, it inferred it, it
distilled it from a job, it proved it in a trial — and measures how often
beliefs from that channel survive. Each line in the notebook carries the verdict:
**strong**, **steady**, or **tentative**, next to its age.

A belief from a weak channel is not allowed to shape a reply on its own; it
needs corroboration first — a second independent recording, a restoration you
performed, or standing evidence in an unsettled pair.

## The competence map

Aforge also measures itself. From real outcomes — jobs that landed, jobs that
failed a delivery gate, and how surprised it was by results — it derives a map
of scopes it is **strong** in, **weak** in, or on the **frontier** of.

"Learning frontier" is concrete: scopes where its failure rate sits in the
middle, or where its surprise is improving, or where there simply is not enough
evidence yet to claim either way. Ask "what are you good at?" or "where do you
struggle?" and the answer comes off that map — it will not claim a strength the
evidence does not show. `aforge competence` prints it from a shell.

## Aging: beliefs it stops using

Every belief is scored by how recently and how often it has actually been used.
One that decays past the retention threshold is **quarantined**: it leaves the
notebook and stops shaping answers. You see `· let go —`.

This is reversible. `aforge notebook restore <seq>` brings it back, and the
original quarantine stays in the record as evidence.

## Correcting a wrong belief

Say so, plainly:

> "forget that — I don't prefer tabs"
> "that's wrong, the staging URL changed"

Aforge finds the numbered belief you mean and retracts it. If more than one line
could be meant, it asks which — it will not guess a number. Retraction is
**quarantine, never deletion**: the belief stops being used, the record of it
having existed stays, and it can be restored.

You can also retract from the notebook panel directly, or with
`aforge notebook retract <seq>`.

## Teaching it something

You do not need a command. State something durable and it is captured:

> "I always want dates in ISO format."
> "Our deploy script needs the VPN up first."

The test it applies is whether the thing will still matter after this
conversation is forgotten. Task details and one-off parameters fail that test
and are not recorded.
