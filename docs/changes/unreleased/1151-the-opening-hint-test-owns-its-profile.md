---
kind: fixed
title: the opening hint test names its own profile and pins the welcome contract both ways
pr: 1151
surface: [chat]
invalidates:
  - "TestTheOpeningHintNamesBothDoors was believed to be a self-contained read of the welcome contract. It named no profile, so its answer came from whatever an earlier test in the same process had written into the package's one shared temporary profile — green in a package run, red alone. Both apps it builds now read a profile the test made, and the marker case is written by the test."
  - "The test asserted the pre-#680 behaviour — a keystroke lands the hint. #680 made the first conversation's greeting stand through typing on purpose, so the assertion was wrong rather than the surface. The test now pins the shipped contract: greeting stands through typing on a profile with no setup_seen_at marker and the hint lands at the send; with the marker written, a keystroke dismisses and the hint lands then."
---

The test built its app with no `ProfileDir`, so `a.profileDir` was empty and
`config.SetupSeenAt("")` resolved through the state root — and TestMain moves
that root to ONE directory for the whole package run (`tui3_test.go`'s
`runTests`, which sets `CODEAF_HOME` and clears `CODEAF_PROFILE_DIR`), so every
test that does not name a profile reads and writes the same one. With
`a.recentSessions` nil, the marker is the whole of `welcome.first`
(`welcome.go`: `len(recent) == 0 && SetupSeenAt(dir).IsZero()`), and an earlier
test in the package had stamped `setup_seen_at` into the shared directory — so
the same test run in the same minute passed inside the package and failed alone,
and the answer it was asserting came from another test's write.

#680 (`f9db3b9c`) made the first conversation's greeting stand through typing on
purpose: the composer must not move out from under the sentence a person started,
so the greeting is spent by the send (`spendWelcome`), not the keystroke. The
test had never been updated for that and still asserted the keystroke path, so
the failing frame was the greeting standing with `› h` in the box — the shipped
contract doing what it says. The surface is unchanged by this fix; the contract
is deliberate and now pinned instead of fought.

Both apps the test builds read profiles the test made, and the contract stands
in two deterministic cases, neither of which reads a profile the test did not
create: with no marker, the greeting stands through typing (the three starting
points are still on the frame under the word typed into the box) and the hint
lands at the send; with `config.MarkSetupSeen` called on the test's own
directory, the same keystroke dismisses and the hint lands on that frame. The
three assertions that were true before stay: the greeting never teaches the exit
before the entrance, a resumed session gets the line on its first frame, and
help names `alt+enter`.

Both roads measured on this tree, in the same hour:

- `go test ./internal/tui3 -run '^TestTheOpeningHintNamesBothDoors$' -count=1 -timeout 600s` —
  green in 0.16s. This was the failing road: the same command on the pre-change
  test went red alone and green only inside the package.
- `go test ./internal/tui3 -count=1 -timeout 600s` — green in 318.2s, one run.

The package-wide coupling that made an empty profile dir mean "whatever the
process's neighbours wrote" is a separate question and is not fixed here.