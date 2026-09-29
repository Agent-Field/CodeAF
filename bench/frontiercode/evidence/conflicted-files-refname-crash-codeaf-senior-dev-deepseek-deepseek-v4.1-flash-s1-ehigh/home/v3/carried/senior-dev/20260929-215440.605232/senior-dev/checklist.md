[x] get_conflicted_files no longer crashes when a tracked file is named like a refname such as HEAD or MERGE_HEAD
[x] refnames (HEAD, MERGE_HEAD) are separated from pathspecs with a `--` separator before invoking `git diff`
[x] the returned set is complete: it still contains both the merge-conflict filenames from MERGE_MSG and the merge-diff filenames, with nothing added or lost
[x] a regression test for the refname collision is added to tests/git_test.py in the existing merge-conflict fixture style
[x] python -m pytest tests/git_test.py passes
