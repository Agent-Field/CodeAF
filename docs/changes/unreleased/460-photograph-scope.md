---
kind: fixed
title: a reading covers what the request names, and is retaken only when the tree changed
pr: 460
surface: [engine]
invalidates:
  - "A reading's focus used to be built from the paths a request spells WITH AN EXTENSION and the identifiers it spells in CamelCase or snake_case, so a request that named a directory — `go test ./internal/subharness/ -count=1`, `packages/happy-dom` — produced an EMPTY focus and a reading of the whole repository. `verify.NamedSubjects` now reads the places a request spells too; `verify.Locate` keeps the ones the workspace holds and drops the ones it does not (prose has slashes in it), and `focusShape` reads a directory as the place itself rather than as a file in its parent, so its package is the scope."
  - "The second reading used to be bought by `changed || inherited`, where `inherited` meant THIS LEAF INHERITED THE JOB'S READING — which every leaf after the first does, whatever it touched. So a job whose leaves changed nothing still ran the suite again at every one of them. `exec.PhotographBefore`'s second return is now `moved`: the job has changed files since the reading was taken, digested by `verify.TreeState` and compared by `verify.TreeUnchangedSince`. Where the tree is unchanged the before reading STANDS as the after reading (`verify.Reading.OnAnUnchangedTree`), journaled as inherited, and no command runs."
  - "`verify.RememberBaseline` took `(root, job, reading)` and now takes `(root, job, tree, reading)`, the tree being `verify.TreeState` of the job's artifact record — an empty digest is an untouched tree. `verify.BaselineFor` is unchanged; `verify.BaselineOf` returns the reading with the state it was taken against. `revision.Evidence.measureFinalTree` takes the job key for the same question."
  - "`verify.Reading.Retakeable` used to be true for ANY cut scoped reading whose pace was known. It now also demands that the retake be a strictly smaller selection, so a reading killed at its budget is never retaken over the scope that was just killed."
  - "`verify.TreeState` is a state of the TREE and not a list of names: every recorded path is settled against the disk — its size and modification time, and a `gone` marker for a path the tree no longer holds. A round that rewrites a file it already recorded, and a round whose only change is a deletion (deletions never reach `exec.Workspace.Artifacts`, which holds what the tree still has), both change the state. `exec` now decides a leaf moved the tree from `Workspace.ArtifactFacts`, which carries deletions, rather than from the artifact list, which cannot."
  - "The delivery gate does NOT answer `nothing to read` for a job with no focus and no artifacts, and an earlier revision of this branch that did was wrong: an empty artifact list is not an unchanged tree, because a deletion never reaches that list, so the answer would hide the regression a removal causes. Where nothing watched the tree the gate reads it, exactly as before this PR; the reading is skipped only where a tree-state the workspace settled says nothing has moved."
  - "A scoped `go test` rung used to be `go test -json ./... ./internal/subharness/...` — the selection appended to a runner invocation that already named the whole tree, so a correctly chosen scope read 4,588 checks anyway. `runner.wholeTarget` names that argument and it is dropped before the selection is appended."
---

One errand — run one package's tests, report the final line, change no files — photographed
the whole repository NINE times: `go test -json ./...` over 4,587 tests, scope `whole`, five
of them actually run and every one killed at its two-minute budget, 82% of an 11m40s wall.
The request named one package. The leaves changed nothing.

Both halves of that were readers answering a question nobody had asked them. The focus reader
was looking for files and for names, and a directory is neither — so the request's own
`./internal/subharness/` named nothing and every rung of the ladder was the whole tree. The
second reading was bought by a leaf having INHERITED the job's reading, which is a fact about
a lookup rather than about a tree; every leaf after the first inherits.

The same errand now takes ONE reading, scoped to the package the request names, with every
later leaf and the gate inheriting it — measured on the same request against the same
checkout at 3m32s instead of 11m40s, with one command run instead of five. PERF.md, "The
verification photograph's budget", states the retake law; no cap moved.
