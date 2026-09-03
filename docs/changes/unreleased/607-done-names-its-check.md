---
kind: fixed
title: a leaf's done carries the commands it ran, or says no check was run
pr: 607
surface: [engine]
invalidates:
  - "`exec.Outcome` had no record of the commands a leaf issued itself. `Outcome.Ran` is a tail of ALL calls, so forty edits displace a check that ran first, and it never reached the account at all. `Outcome.Commands` is the newest `commandsKept` (**20**) shell commands the leaf ran, in order, each clipped at `ranArgumentBytes`, with `Outcome.CommandsRun` counting every one issued so a bounded list can never read as the whole run. A batch refused unexecuted at the output limit records none, because nothing ran."
  - "`exec.Account`'s only account of checking was `Checks`, which is the closing photograph's own reading of the finished tree — so a leaf that ran the suite twice and a leaf that ran nothing produced an IDENTICAL account. It now carries `Account.Commands` and `Account.CommandsRun` beside it, rendered under `What the work ran itself:` and kept apart from the photograph's block. A command there is never evidence that anything passed: nothing parsed its output, and `Account.Verified()` still answers from `Checks` alone."
  - "An account of changed files with no check rendered NOTHING where the check block would be, so a leaf that ran nothing read exactly like one whose checks had nothing to report. It now says `no check was run on the finished tree` — spelled once as `noCheckWords`, in `Account.Report()`, `Account.Lines()` and the one clause `Account.Summary()` hands the plan's state view as `node.Checked`. This is the one deliberate exception to the emptiness law in this area and it is commented as such: the work changed the tree and nothing checked it is a MEASURED fact, not an unknown."
  - "Commands never bring an account into being. `AccountFor`'s law that a run with nothing to account for leaves the account nil is unchanged: they ride an account that exists because the leaf changed files or a reading was taken, and `Account.Empty()` still reads files, checks and the worker's last word."
  - "The command a shell call runs was read in one place — `(*Toolbox).sh` — and any second reader would have had to parse the raw call's JSON back out. `exec.shellCommand` is now that one reading and `exec.shellCommandOf` is it over a raw `ai.ToolCall`, so the tool and the landing record cannot disagree about what a leaf ran."
  - "A finding the closing photograph raised reached NOBODY unless the leaf still had room to be asked about it: `SelfCloseRoom.Left` is false once `Outcome.Exhausted` is set, so a leaf that had already been told to land dropped a measured red into a `\"red\":1` in the journal and the next leaf rediscovered it at its own cost. `exec.Outcome.Standing` is what the leaf lands holding, settled in `land()` — the one seam every exit passes through — by assignment, so a leaf that settled its finding lands holding none."
  - "`exec.SelfCloseFinding` had only `Sentence`, which is the fact plus the move plus the honest alternative, addressed to a leaf that is still standing. It now also carries `Fact`, the finding on its own, and every `Sentence` is composed FROM it — so a handover can name what the reading found without giving an order to a worker that has already stopped, and no person-facing wording moved."
  - "`resident.LeafState` handed the next worker the account and the bounded call tail. It now also names what the leaf's own reading of the finished tree found and it landed holding, between the two."
---

The measured run: a leaf edited a package, wrote two tests, watched them fail,
implemented its fix, ran `go build ./...` — one command, the last tool call in
its whole transcript — and landed. It was recorded **done** with "Now build and
test". Its brief named `go test ./internal/shaped/` twice; that command takes
five milliseconds on that tree and was never run. The next leaf ran it in its
third turn and found the failing test the first leaf had written.

Two silences under one ending. Nothing in the record could say which commands a
leaf had issued, so no reader — the gate, the growth round, or the person — could
tell "ran the suite" from "ran nothing". And the harness's own photograph HAD
measured that red, three minutes before the ✓, into a field only a leaf with room
left is ever shown.
