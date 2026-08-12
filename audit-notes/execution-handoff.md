# Hand-off: execution-side defects from the 2026-08-11 plot incident

For the system that owns workflow/task execution. Full forensics: the
"Stochastic stock fitter with ML" task (task-10539, session
chat-20260811-141209). Evidence chain verified against the journal and trace
logs; every claim below carries its file:line. The UI/head side of the same
incident (absorption turn, promise enforcement, relearned-lesson dedupe) is
being fixed separately — these four are yours.

## Incident in one paragraph

The linear leaf REALLY built and ran the script (36 turns, a genuine
debugging loop) and saved `stochastic_fit_plots.png` (204KB) to the workspace
at 14:19 — before the node completed. The product never learned the file
existed, the `Files:` footer named only the `.py`, the quality gate judged
only the message text, failed the delivery for "not containing the script",
spent a repair round plus a whole continuation node (task-10539-x1, 7 turns of
cat/sed) re-typing a file already on disk, and delivered a 14KB code dump that
tells the user to run the script themselves — while the rendered plot sat in
the workspace. ~$0.075 and 4.5 minutes to retype a correct file; the thing the
user asked to see went unmentioned.

## 1. Linear executor is blind to subprocess-created files  ← the root

`internal/exec/linear.go:803` sets `outcome.Artifacts =
l.workspace.Artifacts(task.NodeID)`, and that registry is populated ONLY by
the write-family tools (`internal/exec/tools.go:1034` write, `:1065` edit,
`internal/exec/media.go`). A file created by `python3` under `sh` is never
recorded. The swe executor already solved this — `internal/exec/swe.go:478`
`recordArtifacts` does a before/after tree scan. Fix: mirror that in linear
(cheapest: diff the workspace around each `sh` call at
`internal/exec/tools.go:944`). Everything downstream — Files footer
(cmd/aforge/chat.go:973), gate evidence, `ExtendForGap` continuation inputs
(chat.go:1163) — fixes itself for free.

## 2. The delivery law contradicts the leaf's own message cap

`internal/plan/delivery.go:57` (DeliveryLaw) demands the whole finished thing
written out in the final message; the leaf prompt at
`internal/exec/linear.go:140-166` demands the final message be the deliverable
AND caps it at ~300 words (`:160`). For a 288-line script both cannot hold —
the worker stalled three times on "I'll write it out completely". Fix: when a
run produced artifacts, the law should be "name them, attach them, summarize";
at minimum drop the word cap when the contract demands full text.

## 3. The gate weighs prose and is blind to artifacts

`cmd/aforge/chat.go:1059-1061`: the judgment saw only
`revision.Evidence{Artifacts: absolute}` (which held only the .py, see #1) and
failed the delivery on message text alone — journal seq 10594: pass=false,
gap="does not contain the full Python script text…". It never asked "was the
asked-for artifact produced?". Fix: when `outcome.Ran` and artifacts exist,
judge artifact-first; a gap closable by a file reference should not buy a
whole continuation node.

## 4. Quality note (separate defect, same trace)

`run.log` shows `Actual future inside 5-95% band: 0% of days`, and trace turn
35 shows the worker hunting random seeds until one "gives a clean path"
instead of fixing the model. The gate passed this. Worth a separate look at
whether the contract can express "the fit must be good", not only "the message
must be complete".

## Context you may want

- The "paste every line into the message" lesson has been learned 5× (fact
  seqs 508, 9256, 10230, 10602 + this incident), each superseding the last —
  the gate failures in #2/#3 manufacture it. The learning-side dedupe is being
  fixed on the UI/head side; fixing #2/#3 removes the manufacture.
- Classification itself was CORRECT by the product's written law
  (cmd/aforge/subharness_swe.go:30 excludes standalone-script work from swe);
  the task ran as one linear leaf via internal/head/compiler.go:276 →
  cmd/aforge/subharness.go:261 → exec.Linear. No action needed there unless
  you disagree with the law.

## Addendum from the craft/head lane (2026-08-11, later the same day)

**5. The craft path has no delivery contract at all.** A craft compiles
straight into store nodes and never passes through `plan.Contracts`, so its
deliverable owner is the one in the product with no acceptance criteria.
Written-and-reverted diff (scope boundary): in
`internal/resident/craftadapter.go`'s `craftRootBrief`, append
`plan.DeliverInMessage` to the root brief (quote the constant, don't restate
it — a second wording is a second law that drifts).

**6. Contract phrasing at the compiler seam** — `internal/head/compiler.go:55`
minimal strengthening, phrased as requirement not example: "…written out in
the worker's own final message; naming the file it was saved in is not a
delivery, and files are named beside that substance rather than in place of
it."

**7. The `" once"` standing-intent exclusion is too broad**
(`internal/head/standing.go:67-73`): it conflates count-"once" with the
temporal conjunction, so "remind me once the deploy is green" can never become
a sentinel. Deliberately not changed (it mints charter drafts from a wide
class of deferred asks = task classification, yours). Proposed discriminator:
treat `once` as standing only when followed by a clause —
`(?i)\bonce\s+(?:it|its|it'?s|they|the|that|this|there|we|you|[a-z]+\s+(?:is|are|has|have|was|were|finish|finishes|lands|completes|passes))\b`.

**8. The cause of the 5×-relearned lesson is the gate loop**: the delivery
gate keeps failing the same way and `distillJob`
(`internal/resident/resident.go:2531`) feeds the gate's gap to the distiller,
which writes the lesson again. The write-side dedupe now exists (lineage-aware,
`internal/resident/retrospect.go:152`), but the manufacture lives in the
gate/contract path (`cmd/aforge/chat.go` judgment + `plan/contract.go`) —
a notebook line cannot make a worker obey a contract it was never given.
