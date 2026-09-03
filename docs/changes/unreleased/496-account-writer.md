---
kind: fixed
title: the landing accounts for its own change, on every belt
pr: 496
surface: [engine]
invalidates:
  - "`exec.Outcome.Account` had no writer anywhere in the tree — the only one left with `internal/exec/swe.go` in 05b99537 — so every reader of it was dead code: the plan's `node.Checked`, the delivery gate's `evidence.Account` and `evidence.Patch`, `revision.composePatch`, the `Landed() && Verified()` predicate, and the bank's `Account.Report()`. It is written now, at the landing seam, for every belt."
  - "The accounting was a property of ONE belt. It is a property of a RUN: `exec.AccountFor` is called from `PhotographAfter` — the one point every belt lands through — and `internal/exec/account_writer_test.go` reads the package's own source to keep it the only place."
  - "`Account.Files` came off a subharness narrating its edits on a wire. It comes off `Workspace.ArtifactFacts` — the before-and-after read of the tree every belt already takes — so a leaf that wrote one file with a shell command accounts for it too. Line counts come from git where the root is a work tree and stay 0 where nothing measured them."
  - "`Account.Range` and `Account.Patch` needed the removed belt's per-node pass refs. The span is now `Opening.Base`..HEAD, with the base read on the same seam that photographs the tree, and the patch is `git diff` over the account's own paths — committed, uncommitted and untracked — written under the harness's own directory and recorded as internal, so it is never offered as something the work produced. Both stay empty where the root is not a git work tree, which reads as no claim."
  - "`Account.Checks` was the engine's own verifier roster and nothing filled it. It is the photograph's second reading: one row for the command that ran, `Known` where every check red now was red before the work began AND the baseline was actually read — never off a reading nobody took, one cut at its ceiling, or one that collected nothing. That is what makes `Account.Verified()` answerable on the ordinary belt at all."
  - "`Account.Patch` had no bound and no argv law. It is capped at `accountPatchBytes` (**1 MiB**), cut at a line boundary with the file saying where, and the pathspec goes out in runs of `accountPathspecBytes` (**96 KiB**) because `git diff` reads no pathspec from a file — both in PERF.md, \"What the change set's own text costs\". Every diff runs with `--no-ext-diff`, so an external diff driver cannot outlive the leaf, and a source git refuses (exit 2 and up) abandons the patch rather than shortening it."
  - "A leaf that changed nothing in a tree its job had not moved keeps the roster it is holding rather than re-running the suite (#460), and that roster stands on `outcome.Verification` as the reading of the finished tree. It is NOT in `Account.Checks`: a roster nobody re-ran is a fact about the tree and not this leaf's claim to have checked its own work, and counted as one it made `Account.Verified()` answer true for a leaf that ran no test — which #464's receipt would have put in front of a person as \"checked by tests\". The retake law is settled once in `PhotographAfter` and read by both."
  - "`Account.Withheld` described work on a private branch in a checkout only the removed belt made. Every belt that lands through this seam works in the shared tree, so it is empty and `Landed()` may say so."
  - "`PhotographBefore` returned `(verify.Reading, moved bool)` and `PhotographAfter` took both. Both now carry one `exec.Opening` — the same reading and the same `Moved` from #460, with their meaning and their rule about when a second reading is bought unchanged, plus the commit the repository stood on when the tree was photographed, which is the one fact that cannot be recovered at landing."
  - "`cmd/aforge/chat.go` discarded the error from `store.RecordDeliveryGate`, so a delivery gate the store refused vanished from the ledger with nothing anywhere saying a delivery had been judged. The write is checked now and a failure is logged. The store's own refusal of a failing gate that names no gap is unchanged here and is being carried separately: recording it needs a field of its own, because every reader of `DeliveryGate.Gap` treats a non-empty gap as a defect to repair."
---

The account was written to end an expensive silence — a worker that could only
hand back a sentence and a bill — and then the only thing that ever wrote one
lived beside a single belt. The belt was removed and nothing noticed for a
hundred commits, because every reader of an account treats nil as "no claim":
each of them was quietly, correctly reporting that nobody had looked, forever.

So the repair is the same one `internal/exec/photograph.go` already carries in
its header, one field over: the measurement moves to the one seam every belt
lands through, and a structural test over the package's own AST is what makes
the next removal loud.
