# Real issues, as one more scenario

A real-issue cell is one closed bug from a real repository, the tree frozen at the
commit **before** its fix, the issue text as the brief, and the test the fix brought
with it held out as the mark. Nothing here runs anything: the battery in
`bench/conversation` already has the arms, the two doors, the allowlist guard, the
receipts and `campaign.py`'s freezing. This directory is the part that decides whether a
candidate is honest enough to be run at all.

```sh
fixtures/realissue/manifest.py validate candidates/*.json
fixtures/realissue/manifest.py preflight candidates/*.json --repo /path/to/checkout
python3 -m unittest discover -s bench/conversation/test -p 'test_realissue*.py'
```

`validate` is offline and needs no git — it is what the unit tests run. `preflight` adds
what needs the object store: the base commit exists, the fix commit sits directly on top
of it, and the held-out test is absent at base and present at the fix.

## The rules the manifest holds

- **The base is frozen.** Full 40-character shas, never a branch or a tag, and never the
  same commit as the fix.
- **No future history in the workspace.** `git archive` (or one fresh commit), so there
  is no `.git` to read the answer out of — `carries_git_history: false` is required, not
  a default.
- **The mark is held out.** The acceptance test comes from the fix commit and is copied
  in after the harness has stopped, from outside the workspace.
- **The brief does not carry the fix.** No fix sha, no reference to the fixing pull
  request, no held-out path or basename, no diff — in the prompt or in anything written
  beside it. `525-one-reading-for-the-day.json` is the worked example of failing this:
  the issue itself names the test file the fix would add.
- **Doors and arms are paired both ways.** Every arm is named by a door, every door names
  arms this suite can actually drive — `opencode` has no interactive door here — and the
  model pin is an exact open-model id.
- **A readiness word is a claim about evidence.** `calibrated` requires both halves —
  the held-out test observed failing at base and passing on the reference fix — each with
  the command, the date, and `build_ok`. **A test that fails to build at base is not
  acceptance**: it grades the reference implementation's shape, so a correct independent
  repair scores zero. That is why `370-usage-frames-accumulate.json` is rejected, with
  the compiler's own words in the file.
- **Nothing here promotes anything.** `preflight` reports; a person writes the
  observation into the manifest.

## What is not here

No runner, no scenario wired into the battery, no campaign change, and — apart from
`528-reopened-conversation-jobs.json` — no calibrated candidate. The rest of the picture,
including contamination and the fairness cost of benchmarking aforge on aforge's own
repository, is in the review this directory was delivered with.
