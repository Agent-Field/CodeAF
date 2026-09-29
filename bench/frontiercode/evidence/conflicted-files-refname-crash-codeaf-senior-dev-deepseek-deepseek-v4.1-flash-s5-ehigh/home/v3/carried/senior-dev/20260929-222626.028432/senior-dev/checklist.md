[x] `get_conflicted_files` separates refnames from pathspecs (trailing `--`) so `git diff` no longer dies with "ambiguous argument" when a tracked file is named `HEAD` or `MERGE_HEAD`.
[x] The enumeration still returns the complete set (merge-conflict filenames plus merge-diff filenames), nothing added and nothing lost, in the presence of such a collision.
[x] A regression test covering the refname collision was added to `tests/git_test.py` following the file's existing fixture style.
[x] `python -m pytest tests/git_test.py` passes.
