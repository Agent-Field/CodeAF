# The seed list for a second wave

Six more rows from the cold-user trial's first item. **They are NOT in
`LEDGER.md` and #518 is not reopened for them** — that ledger is closed at 181
of 181, and a wave whose scope moves while it is being reviewed cannot be
reviewed. This file is the next wave's starting point.

**Every row here is split**, because most of them are two defects wearing one
sentence. The half this surface owns is WHAT THE PRODUCT SAYS AND SHOWS. The
engine half — why the thing happened at all — is being filed separately, and a
lane that takes a row here should take only the half under `shows:`.

Each row records what I checked against `ui/polish-v0` before writing it down,
because three rows in the last wave turned out to name the wrong thing, and each
of those cost a lane a day.

---

**N1 — the gate refused, and its refusal named a file change that never
happened.**
`shows:` a refusal is the most-read sentence this product writes, and one that
describes work that did not occur is worse than no refusal: the person goes
looking for the change. The surface half is that the refusal text is printed
without anything having checked it against what is on the tree.
`engine:` why the gate believed a file changed.
*Not verified here* — I have no capture of it. **The first job on this row is to
reproduce it and keep the frame**, not to start editing refusal text.
— sev: high

**N2 — `do --json` carries no `exit_code`, and the stream never prints one.**
`shows:` verified on this branch — the envelope has `ok`, `stop`, `error` and
fifteen other keys, and no number. `stop` is the better field for a program to
branch on and it is what the manual teaches, but a `--json` object CAPTURED TO A
FILE has lost the process's exit status, and the exit ladder is the thing every
script's caller compares. The mapping is deterministic (`exitFor`), so the number
is free.
`engine:` none. This one is entirely ours.
— sev: med

**N3 — a refused run leaves nothing on the tree despite `-w`, and to see what it
understood you must already know `aforge why` and the store path.**
`shows:` the refusal is a dead end. Something that stops without writing has to
say where its record is and how to read it — the developer trial has already
shown twice that a person will go hunting in the wrong directory rather than
guess. This is the same shape as `--debug` naming a folder nobody could find,
which is closed in #518, and the fix is the same one: name the door in the
sentence.
`engine:` whether a refused run ought to leave something on the tree at all.
— sev: high

**N4 — the leaf contract says "Read CLAUDE.md first" and the leaf has no read
tool.**
`shows:` **I could not find that sentence anywhere in the tree**, which matters
for whoever takes this: it is not a fixed string to go and edit. It is written
into a leaf's brief at RUNTIME, so the row is about a brief that tells a worker
to use a capability it was not given — and this repository's own law is that a
capability which cannot work is ABSENT, not broken, so the belt and the brief
have to be built from one list. The surface half is only whether a person is ever
shown that a leaf was told to do something it could not.
`engine:` the brief and the belt disagreeing.
— sev: med

**N5 — a token-budget overrun of 1.7x goes unnoticed until the settle line.**
`shows:` the budget is a limit somebody SET, so passing it is news, and news
arrives when it happens rather than in the receipt afterwards. The live status
line already carries context and spend and already has the reservation machinery
(#518's `costCell`) to grow a segment without shoving the cluster sideways.
`engine:` why an overrun of that size is possible.
— sev: high

**N6 — the settle line hides that about 12% of spend went on 429 retries and
hedge waste.**
`shows:` one number for "what this cost" and no way to tell work from waste. A
person tuning their spend cannot act on a figure that mixes them, and the
emptiness law is no help here because the waste is not zero — it is real money
with a cause. The question to answer first is a design one and it belongs to
whoever takes the row: is retry-and-hedge waste a SECOND FIGURE beside the
spend, or a clause under it, or a fact the spend page owns rather than the
settle line?
`engine:` the retry and hedge behaviour itself.
— sev: med

---

## What the last wave would tell this one

- **Reproduce before editing.** Three rows in #518 named the wrong thing — a
  glyph that would only be that letter if the product still had its old name, an
  emoji class that never caused the bend blamed on it, and a mechanism I wrote
  down myself and got wrong. All three were caught by looking at the code or the
  frame instead of the row.
- **Revert every fix and watch its test fail.** Six tests in #518 passed against
  the very defect they named. Nothing else caught any of them.
- **A cold user is worth more than an audit.** Seven findings in one afternoon,
  four of them real, none of which twelve audits had found — because everybody
  else already knew what the program meant to do. Run the trial BEFORE the wave.
