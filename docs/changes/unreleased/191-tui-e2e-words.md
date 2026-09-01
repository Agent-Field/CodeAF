---
kind: fixed
title: the tmux TUI suite runs again — 9 of 9, with one table of words guarding every needle
pr: 191
surface: [chat, build]
invalidates:
  - "`go test -tags e2e -run TestTUIE2E ./internal/e2e/` was 2 of 9 on a clean tree and had been since the home redesign, so anybody who ran it read seven failures that were the TEST being stale. It is 9 of 9 now, and it is the way to verify a wave against a real terminal and a real model: `go test -tags e2e -count=1 -timeout 40m -v ./internal/e2e/`, with OPENROUTER_API_KEY and tmux, a few cents, about seventeen minutes. CLAUDE.md's Tests section says so."
  - "`internal/e2e` held no untagged Go file at all, so `go vet ./...` and `go test ./internal/e2e/` both failed with `build constraints exclude all Go files in …/internal/e2e`. That was a red nobody had written down. The package has an untagged test file now and both pass."
  - "The tmux suite's needles were forty string literals typed into the test. They are one table — `internal/e2e/tuiwords_test.go` — reached only through `say(t, \"<name>\")`, and two untagged tests hold both ends of it: one parses `internal/tui3`'s non-test sources and fails when a word the suite waits for is no longer spelled there, the other reads the suite back and fails on a table row nothing waits for. Respelling a person-facing string in tui3 now names you in under a second, on your own pull request."
  - "The suite believed home was a tree: `esc close` in the head, a `─ elsewhere` rule, folded `▸ delta` project lines, a conversation card carrying a left-off band and a `enter open · n new chat here` keys band, a `◦ …` standing row under a project. None of that has existed since the home rethink. Home is one flat ranked list, the project is a tag on the row, the card carries five bands and the repository fact folded into its place line as `main, 1 file dirty`."
  - "Every subtest ran at 120 columns, and the ones that read a card were therefore asking for something the product correctly does not draw: there is no card at all below 160 (`homeCardMin`). The card subtests run at 180 now, and the file says why."
  - "`m_toggles_the_folds_on_a_card` and `the_project_card` tested two things the rethink removed — the card's news band with its `▸ …N more` door, and the folded project line that was the only cursor stop carrying a project's card. They are replaced, not deleted, by the two surviving halves of the same ideas: the one fold at the foot of the list (`→` opens, `←` shuts) and `alt+g` grouping by project."
  - "`the_firing_reaches_the_person` waited for the person's own sentence on the fired row. The MODEL names a standing order and writes what it says when it fires — one run called the same order `drink water reminder` and fired `💧 Drink water!` — so the suite reads the record off disk and takes its needles from there."
  - "`answer_from_home_across_two_windows` proved the answer landed by waiting for the turn's `1 tool call` fold. A model is free to reach for bash a second time, and a window drawing a second question says nothing about the first; the claim is carried by the journal now, which nothing but the answer could have filled."
---

A suite that fails on a clean tree is a suite nobody runs, and this was the only
test that drives the real binary in a real terminal against a real model. It had
been stale for a week and the cost was not the seven red lines — it was that no
wave in that week could check the surface at all.

So the repair is not only the rewrite. Every string the suite waits for now lives
in one table with a reason beside it, and an ordinary untagged test reads that
table back against the code that owes each word. It needs no tmux, no key and no
build tag, so it runs in the pull-request gate: the day somebody respells `kept ·
this exchange is filed under it`, the failure names the string, the file and the
subtest, on the change that did it, instead of seventeen minutes and a few cents
finding out weeks later.

Two product defects turned up while it was being rewritten and are filed rather
than fixed here: `stand` accepts an expiry at or before an item's own moment, so
a one-off reminder retires without ever firing (#188), and the errand pane's hint
offers a `3 just once` the card did not draw (#189).
