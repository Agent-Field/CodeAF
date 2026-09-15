---
kind: fixed
title: The folder walk's budget bounds every entry, and the suite never walks your home
pr: 1058
surface: [chat]
invalidates:
  - "`folderIndexWalk`'s three-second budget was believed to bound the walk. It did not. The clock check stood BELOW the `!entry.IsDir()` return, so a file never consulted it and the walk got a turn to stop only between directories — and the comment over the constants said exactly that (\"checked before traversal and at each directory\"), which was the defect written down rather than a description of it. One directory holding hundreds of thousands of files (`~/Library/Caches` on any Mac) therefore ran unbounded: measured on the owner's laptop, `scanFolderRoots` took 5m58s under a 3s budget and returned 1144 roots. If you remember the folder index as cheap, it was cheap only on a small home directory."
  - "`internal/tui3`'s test suite walked the REAL home directory of whoever ran it. tui3_test.go's TestMain moves the state root and deliberately leaves HOME alone, because the package draws `~` in front of paths — but `scanFolderRoots` does not draw `~`, it walks it with os.UserHomeDir, which that seam never covered. Any test opening the folder picker paid for a full home index. `folderRootScan` is now the seam and TestMain answers it with nothing, so CODEAF_HOME is once again the whole story about what a tui3 test touches."
  - "`internal/manual/chat/choosing-a-folder.md` described layer 4 of the picker as an index of repositories and ordinary folders under your home directory and named no limit. It now states the three stops — six levels, two thousand folders, three seconds — because a walk that genuinely stops is a list that is genuinely partial, and typing a path is the way past it."
---

The two halves are one change because the second is what made the first
visible. A budget that silently failed to bound anything is invisible until
something with a budget of its own is waiting on it, and the thing waiting was
the test driver: `TestThePicksLandingMidBrowseKeepTheChoicesAndThePreview`
panicked after five seconds on a laptop and passed on a CI runner whose home
directory is empty.

`TestTheBudgetIsMeasuredAgainstEveryEntryAndNotOnlyDirectories` drives the clock
rather than racing one. What is being proved is WHERE THE WALK STOPS, and a
threshold in wall clock is a coin toss on a loaded box — so the clock spends a
second of the budget per reading, and the assertion is that a later directory is
never reached across a run of files.

`folderIndexWord` already writes `there was more to look through than this list
holds` for precisely this case and nothing draws it. That is left standing:
wiring the sentence onto the picker is a different change from making the number
behind it true.
