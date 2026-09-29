# Checklist

[x] `get_conflicted_files` disambiguates the refnames from pathspecs so a tracked file named `HEAD`/`MERGE_HEAD` does not crash the git invocation.
[x] The enumeration still returns the complete set: merge-conflict filenames unioned with merge-diff filenames (nothing added, nothing lost).
[x] No error is swallowed and no empty/partial result is returned in place of the crash.
[x] Regression test in `tests/git_test.py` rides on the existing merge-conflict fixture and covers a tracked file colliding with a refname.
[x] `python -m pytest tests/git_test.py` passes.
[x] Diff stays focused and conforms to the repo's style (single quotes, no f-strings).
