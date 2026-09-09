# Real issues, as one more scenario

A real-issue cell is one closed bug from a real repository, the tree frozen at the
commit **before** its fix, a brief **derived** from the issue, and the test the fix
brought with it held out as the mark. Nothing here runs anything: `bench/conversation`
already has the arms, the two doors, the allowlist guard, the receipts and `campaign.py`'s
freezing, and `bench/deepswe` drives container-graded corpus tasks (its own chat door is
PR #634). This directory is a **supplement to both**, not a runner: it decides whether a
candidate is honest enough to be run at all.

```sh
fixtures/realissue/curate.py <issue-body>          # derive a brief, print its record
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
- **The brief is derived, and says so.** An issue here is an engineering report:
  `## Where` is the diagnosis, `## The fix` carried a literal Go patch, `## Acceptance`
  named the held-out tests. `curate.py` keeps symptom, reproduction and the stated law by
  heading, drops named paragraphs by pattern — a report writes its diagnosis wherever it
  likes — and records kept sections, dropped sections, every drop pattern with its count,
  the source body's sha256 and the prompt's own. `verbatim: false` is required and a
  prompt whose hash has moved is refused.
- **The lint is lint, not proof of isolation.** It refuses a `## The fix` heading, a fenced
  block of the implementation language, a `Test…` function name, a fix sha, a
  pull-request pointer, a diff, a held-out path, and any path the repair must edit. It
  cannot prove a paragraph does not describe the repair; that rests on curation and on a
  person reading the result. `525-one-reading-for-the-day.json` is the worked example of a
  body curation cannot save — its surviving sections name the held-out test and its
  function.
- **Guards protect instruments, never implementation.** `fix_paths` names the paths the
  repair is known to need, and neither `guarded_paths` nor `held_out_paths` may intersect
  it. Guarding the file the agent has to edit rejects every real fix.
- **Every compared door carries every arm.** Not the union: a print row for one arm and an
  interactive row for another is two experiments printed as one comparison. Arms are
  unique, and a door names only arms this suite can drive — `opencode` has no interactive
  door here. The model pin is an exact open-model id.
- **A readiness word is a claim about evidence.** The ladder is
  `rejected → candidate → preflight-clean → grader-calibrated`. `grader-calibrated` needs
  both halves — failing at base, passing on the reference fix — each with the command, the
  date, the **exit code**, the **sha256 of the captured output**, `build_ok`, and
  `verification: manual-recorded`, because nothing here re-runs them. Polarity is checked
  offline: a "failure" recorded with exit 0 is refused. **Grader-calibrated is not
  campaign-ready** — no arm, cap or door evidence lives in this file, so
  `campaign_ready: true` is refused outright.
- **Scope, v1: `task_kind: bug-existing-api`.** Within that kind a held-out test that does
  not build at base is refused, because it needs a symbol the fix introduced —
  `370-usage-frames-accumulate.json` demands the unexported `mergeUsage` and
  `usageWire.mergeInto`, so a correct independent repair scores zero. This is **not** a
  universal rule: a feature task may legitimately need a public API that does not exist at
  base. That is a different kind and this file does not accept it yet.
- **Nothing here promotes anything.** `preflight` reports; a person writes the
  observation into the manifest.

## What is not here

No runner, no scenario wired into the battery, no campaign change. Only
`528-reopened-conversation-jobs.json` is grader-calibrated, and it is **twelve lines of
repair in one file** — it calibrates the grader and stands for nothing about complex
issues. There is no regression side: the acceptance command runs the held-out cases only,
so nothing checks that the rest of the package still passes. Contamination is not settled
by the repository being private, and benchmarking aforge on aforge's own repository is a
fairness asymmetry that has to be published with any number. The review this directory was
delivered with carries the rest, including the DeepSWE corpus on Spark and PR #634.
