---
kind: fixed
title: the chat surface reads the environment through one seam, so a test can hand it a table
pr: 417
surface: [chat, build]
invalidates:
  - "`internal/tui3 TestAPathInsideACorrectionIsADoor` was believed environment-shaped red on Linux and skipped by CI. The surface read TERM straight from the process to decide whether a path may be written as an OSC 8 link, and GitHub's runners export no TERM; it reads through `Options.Env` now, `newTestApp` hands it a table (`TERM=xterm-256color`, no TMUX, no SSH vars), and the line is out of `.github/known-red.txt`."
  - "`newApp` read `os.Getenv` in four places — `tmuxTerm`, `remoteLink`, `detectChords`, `terminalTakesLinks` — and `newTestApp` pinned three of the results by hand after construction. `Options.Env` is the one place the surface reads the environment, nil means `os.Getenv`, and the `a.tmux = false` and `a.remote = false` pins are gone; the chord pin stays because `detectChords` also reads GOOS."
  - "Three more tui3 tests (`TestTheSweepCopiesExactlyWhatItLit`, `TestAnAnswerAskedFromHomeIsRenderedMarkdownAndNotItsSource`, `TestAHomeAnswerIsRawWhileItFormsAndStyledOnceItSettles`) were thought to fail with no TERM for the same reason. They do not: they fail through `internal/tui3/markdown.go`'s package-wide styler, a `sync.Once` over `os.Getenv`, which this change does not touch. They still fail with TERM unset, are still in no ledger, and are owed a second seam. `TestADropTypedSlowlyStillLands`, named in the same CI run, passes with TERM unset and is not TERM-shaped."
---

A test that reads the developer's own terminal is a test of that terminal: inside
tmux it saw the wrapped clipboard write, over ssh it stepped every animation three
slots at a time, and on a CI runner with no TERM it wrote no links and failed the one
assertion that looked for one — on exactly the machine where nobody was watching.
`newTestApp` had learned this three times and pinned each answer after the fact; the
fourth read was the one it had not learned. One seam at the door, read once, is what
lets a suite state its terminal instead of inheriting one. CI evidence: run
33645143035 on PR #401 (ubuntu-latest at 22b7777d).
