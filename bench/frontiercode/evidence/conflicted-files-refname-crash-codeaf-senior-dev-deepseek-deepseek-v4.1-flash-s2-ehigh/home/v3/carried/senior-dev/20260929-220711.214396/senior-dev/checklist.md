[x] Refnames are separated from pathspecs in get_conflicted_files so `git diff` treats HEAD/MERGE_HEAD unambiguously
[x] get_conflicted_files no longer crashes when a tracked file's name collides with a refname (HEAD / MERGE_HEAD)
[x] The enumeration still returns the complete set: merge-conflict filenames AND merge-diff filenames, nothing added or lost (colliding file is returned, not an empty/partial result)
[x] Regression test added in tests/git_test.py following the existing fixture style, covering both HEAD and MERGE_HEAD collisions
[x] Existing tests in tests/git_test.py keep passing and the diff is style-clean (lines <= 79 chars, single-quoted strings)
