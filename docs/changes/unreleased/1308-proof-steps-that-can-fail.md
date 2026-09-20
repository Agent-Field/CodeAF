---
kind: internal
title: three proof steps that could not report failure now have one written form with a tested failing arm
pr: 1308
surface: [build]
invalidates:
  - "A proof step written the obvious way can be incapable of reporting the failure it is checking for, and three such shapes were each written more than once. `gofmt -l` prints what it would reformat and exits 0 either way, so a step reading its exit code reports green on every tree. A pipeline ending in a reader hands back the reader's status, so `cmd 2>&1 | tail -25; echo \"exit=$?\"` printed `exit=0` underneath a screen of FAIL. And a `-run` pattern is a second copy of the list of tests in a file; when the two drift, `go test` answers a filter matching nothing with `ok`, so a named test that never ran reads exactly like one that passed. `scripts/proof.sh` is the one written form of all three, to be sourced."
  - "No caller is converted in this change, on purpose. If the library is right the conversions are mechanical, and if it is wrong that is better learned on one caller than on all of them."
---

Every proof step answers one question: what would this print if the thing it
checks were broken? If the answer is the same thing it prints now, it is not a
check, and it spends the attention a real check would have earned.

`fmt_check` reads the list rather than the exit code. `step` captures its
command's status before anything else can run, keeps output in a file rather
than a pipe, and counts failures from that file as a second witness sourced
differently from the status. `run_tests` compares the names it was asked for
against the tests that actually reported, and names every one that reported
nothing, because a guard whose satisfaction can only be shown as a tick
eventually becomes a tick.

**The acceptance is the failing arm.** `internal/ci/prooflib_test.go` runs each
function twice, once where the thing it checks is sound and once where it is
broken, and each broken arm also pins what the plain command says about the
same situation: in all three cases it reports success. A guard against invisible
failure that has only ever been seen passing is the thing it was written to
prevent, wearing its own name.

The three shapes were found in one night, each by a different person, each in a
different instrument. The review question and the test-shaped version of the
same family are in the issue titled "Tests that cannot fail".

**The survey, with its scope attached.** Three defects were looked for, in three
places: a `gofmt -l` whose exit code is read, a status reported after a pipe, and
a `-run` pattern listing named tests that can drift from a file, across
`Makefile`, `scripts/` and `.github/`. Zero instances of all three.
`fmt-check` was already written in the correct form before any of this. Every
instance found was in an ad-hoc chain script written outside the repository.

That count is a claim about those three shapes in those three directories and
nothing else. It says nothing about any other way a failure can be discarded, and
nothing about Go source, which was not looked at. A finding quoted without its
scope becomes a larger claim than the one that was tested, which is the same
defect as a check that cannot fail, facing the boundary instead of the content.

This is true of any count and worst of a zero. A zero is the most quotable number
there is and the most dangerous, because it reads as a property of the tree
rather than as the result of a search, and it is the one nobody thinks to ask the
scope of: zero feels like the absence of a thing rather than the outcome of a
procedure that had a boundary.

So these functions may have no caller here, and the file's standing value is its
test rather than its convenience: three assertions about how the tooling behaves
that our proof chains depend on, which run on every pull request whether or not
anything sources a line of it.
