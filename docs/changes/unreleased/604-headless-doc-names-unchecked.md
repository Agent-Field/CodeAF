---
kind: fixed
title: docs/HEADLESS.md names the `unchecked` ending, and three probes hold the page that explains it
pr: 604
surface: [docs]
invalidates:
  - "`docs/HEADLESS.md` said `aforge do` leaves on exit 2 as `incomplete` and listed eight `stop` words — `done`, `error`, `incomplete`, `budget`, `turn-cap`, `deadline`, `price`, `question`. The binary has spoken nine since #593: `unchecked` is exit 2 as well, and it means the work was delivered and the final check could not be reached, so nothing has vouched for it. The page's exit table, its `stop` row and its field table now say so."
  - "`do --json`'s field table in `docs/HEADLESS.md` had no row for `unjudged`. It has one, and it states the rule that field is written under: the key appears on exactly the runs nothing checked, so its presence is itself the answer to \"was this checked?\" and a caller never has to read the sentence."
  - "Nothing in `internal/manual/chat_test.go`'s probe table held a question to the chat manual's account of that ending, so its retrieval rested on no gate — the shape the lanes page was in before #453. Three probes hold it now, in the words somebody meets the ending in: the stderr line they have just read, the word in `--json`, and the exit code they are staring at with a good-looking answer above it. All three already landed; no page was moved to make them."
---

The behaviour is #593's and did not change here. What changed is that the two
documents a person and a harness read about it now agree with the binary: the
machine-facing one names the word, and the chat's own corpus has a gate holding
the question to the page that answers it.
