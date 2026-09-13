---
kind: changed
title: The binary's budget is 57,488,000, and PERF.md says what spent the 1.9 MB that crossed the old one
pr: 726
surface: [build, docs]
invalidates:
  - "`SIZE-BUDGET` was 54,600,000 and `make check` had been red at its last step on `dev` since #653 — for every change, whatever it touched. It is 57,488,000, two percent above darwin/amd64 at 56,360,432, and `make check` runs to the end again."
  - "The refusal `make size` prints, and the informational size job in `ci-full.yml`, both said the budget was set on linux/arm64. It has been set on darwin/amd64 — the heaviest of the four platforms — since #235, and both now say so. Anybody who reset the number against linux/arm64 because the message told them to was resetting it against an architecture it was never measured on."
  - "That CI comment also said the runner's number and the budget 'are not comparable'. They are: linux/amd64 weighs about eight hundred kilobytes less than darwin/amd64, which is exactly why the job stays informational — a hard compare there would pass a tree that is over the cap on the platform the cap is measured on."
  - "`PERF.md`'s size-ratchet section ended at the fourth reset (#235, the first one downward). It carries a fifth: the per-merge attribution from #624 to #721, the section-by-section split of the growth, the dependency weights that were checked before the number moved, and the four-platform table the figure was taken from."
---

The ratchet did the job it exists for: a year's habit of quiet accretion arrived
as one line somebody has to sign. #624 was the last merge under the cap; by
#721 the binary had grown 1,908,736 bytes, of which #653 is 1,601,536 and
crossed the cap by itself. The rest is twenty-odd merges, none over 120 KB.

`PERF.md` allows two honest moves and the first one was looked for first. The
range added no dependency — the only `go.mod` change moves `rivo/uniseg` from
indirect to direct and it was already linked. The one embed that grew is the
chat manual's own pack, 707,105 to 782,330, already gzipped and required by the
manual law. What is left is `.text` +1,030,464 and `.gopclntab` +592,718 of
compiled first-party code, against +26,203 net non-test lines in
`internal/tui3` and +13,011 in `internal/session`. The three dependencies that
weigh anything — `modernc.org/sqlite`, `dop251/goja` and the `x/text/collate`
tables goja pulls with it — each sit behind a shipped capability, and taking all
of goja would still have left darwin/amd64 over the old number.

Nothing a person can see in the running program changes. This is a number, its
paragraph, and two messages that had been naming the wrong architecture.
