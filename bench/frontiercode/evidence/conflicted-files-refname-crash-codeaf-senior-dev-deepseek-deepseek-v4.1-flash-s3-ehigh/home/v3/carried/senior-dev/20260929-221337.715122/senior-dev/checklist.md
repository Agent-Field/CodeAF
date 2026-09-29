[x] get_conflicted_files no longer crashes when a tracked file's name collides with a refname (HEAD / MERGE_HEAD)
[x] refnames are separated from pathspecs in the git diff invocation so the refs are unambiguously revisions
[x] enumeration still returns the complete set: merge-conflict filenames AND merge-diff filenames, nothing added or lost
[x] a regression test using the existing merge-conflict fixture is added in tests/git_test.py
[x] the fixed command produces no empty-but-successful result and does not swallow errors / return a partial set
[x] python -m pytest tests/git_test.py passes
[x] diff is focused and follows file conventions (single-quoted strings, flake8-compatible lines)
