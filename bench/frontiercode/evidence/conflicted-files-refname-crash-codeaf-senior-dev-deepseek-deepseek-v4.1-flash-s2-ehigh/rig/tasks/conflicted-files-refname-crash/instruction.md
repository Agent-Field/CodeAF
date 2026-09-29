# Do not crash when a tracked file shares a refname

## Task description

This repository is a git hook framework. When a merge is in progress it
enumerates the merge's conflicted files so hooks can skip or handle them. The
enumeration reads the merge's conflict list from `MERGE_MSG` and then runs a
`git diff --name-only -m <tree> HEAD MERGE_HEAD` over the user's worktree to
collect the rest of the merge's changes.

There is a latent crash: those refnames are passed to `git diff` bare. If the
worktree happens to contain a tracked FILE whose name collides with a refname
(`HEAD`, `MERGE_HEAD`), git cannot tell the ref from the pathspec and the
whole command dies with `error: ambiguous argument` — taking the tool down at
the worst possible moment, mid-merge.

Fix it: the enumeration must not crash when a tracked file's name collides
with a refname, and it must still return the complete set — the
merge-conflict filenames AND the merge-diff filenames, nothing added, nothing
lost. Separating the refnames from pathspecs is the natural shape; beware the
silent failure: an empty-but-successful result is worse than the crash it
replaced, and catching the error to return a partial set is not a fix.

The repository to work in is `~/repos/pre-commit`, already checked out for
you. Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project is a standard Python package, installed editable into the image's
environment:

- the tests that cover this change live in `tests/git_test.py`, which drives
  real git in temporary repositories (the suite has its own merge-conflict
  fixture the new behaviour should ride on)
- run the relevant tests with `python -m pytest tests/git_test.py`
- if you add a regression test, follow the file's existing fixture style

Tests that cover your change must keep passing.

## Style guidelines

- keep the diff focused on the change; the project's own pre-commit hooks and
  its flake8 config run over the tree
- follow the file's existing conventions (single-quoted strings, no
  f-strings where the file does not use them)

---

## Internet use

This task runs with internet access. What it is for: reading documentation,
API references, error messages and background concepts — the material any
engineer would consult while doing this work on their own machine.

What it is not for, and what we scan for after every run: anything that could
hand you the fix for this specific task. That means the repository this task
is cut from, and any mirror, fork, vendored copy, issue tracker, pull request,
commit, patch, diff, changelog or CI log of it; and searching for phrasings
likely to surface the bug or the patch itself. Finding this task's own
upstream change is a flagged run: it scores zero regardless of the rest.

If a page you land on turns out to be from the task's own upstream project,
stop using it, say so in your final answer, and carry on from what you already
had.
