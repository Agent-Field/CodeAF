---
kind: fixed
title: a constraint the person states is a law of the run, held at the gate and never advice
pr: 461
surface: [engine, resident]
invalidates:
  - "A constraint in the request used to reach the run only as prose inside the goal and the compiled working method — the brief had no field for one, and the acceptance checklist rightly holds behaviours of the RESULT rather than rules about the run. The compiled `head.Brief` and every `plan.Spec` now carry `constraints` as a field: the person's own words, a mechanical reading (`no_writes`, `paths_only`, `other`), and the paths that reading is about. `plan.Graph.SetConstraints` stamps EVERY node, unlike `SetAcceptance`, which stamps only the node that delivers."
  - "The delivery gate used to judge only absences — a file the plan promised and the disk does not hold, a behaviour nothing exercises, a check the run turned red — and never held the workspace's own before-and-after list against a rule about what the run may not touch. A broken `no_writes` or `paths_only` constraint now fails the delivery MECHANICALLY, above every other finding and before the judge is called, and buys no repair round and no remainder: `revision.ExtendForGap` refuses before anything is planned, the repair round is skipped at the wiring seam, and `store.DeliveryGate.Whole` counts `Constraint` as leaving the delivery short — no pass, no closed repair and no overturned refusal covers it."
  - "`revision.RepairClosed` used to read 'the round changed nothing on disk' as not repaired on EVERY job. It is neutral now on a job whose request states a `no_writes` constraint: a run told to change nothing that changed nothing has kept its word, and the standstill rule is written for runs that were supposed to move."
  - "A leaf used to read no rule of the person's at all — `plan.Spec.Render` is not what the generic worker is handed. `cmd/aforge.leafContract` now puts the rules above the working method, which is the first block of the worker's brief, and a repair round is handed them under \"Rules the person set, which outrank anything below\" ahead of the reviewer's gap."
  - "Nodes spliced AFTER the plan used to carry nothing of the job's rules: the remainder planner built from a goal and a terrain alone, and a claim-time expansion minted a fresh sub-graph that inherited the settled points and not the constraints. Both carry them now — `replanRemainder` reads them off the node it is continuing, and `plan.ExpandOne` brings them down with the settled points."
  - "A claim-time expansion used to mint children with an ENTIRELY EMPTY `plan.Spec`: it inherited the graph's premises (settled points, terrain, invoice) and nothing whatever off the node it divided, so a divided node lost its criterion, its working method and its rules at once. `plan.Graph.mintedInside` now puts every minted child inside its parent's whole spec — Done and Method fill what the sub-plan left empty and never overwrite what it wrote, Constraints go on every child, and Accept still answers to `deliverableOwner`."
  - "A constraint the MODEL JUDGE convicted on used to be an ordinary gap and bought the repair round and the remainder like any other. `revision.ConstraintQuoted` now stamps a refusal whose quote is contained in a stated `other` rule as `FindingConstraint` at the gate's one exit, so it buys nothing either."
  - "The quorum repair round commits unconditionally with no second gate, so it could break `no_writes` and ship over a pass. `revision.ConstraintsHeld` is re-taken over what that round left behind, and a broken rule rejoins the ordinary failed path."
  - "The delivery gate's constraint reading used to drop any recorded path it could not `os.Stat`, so a run told to change nothing could DELETE a file or leave a dangling symlink and pass. A path the record holds is now a change whether or not it is still there; only a directory is skipped. It also read any root-level name beginning with two dots (`..config`) as outside the workspace, which is now only `..` and `../…`; containment is lexical and a symlinked root is not resolved."
  - "`revision.RepairClosed`'s new `no_writes` neutrality does not cover a MECHANICAL finding: a file the plan or the person promised and the disk does not hold still needs the disk to move, whatever the person forbade about writing."
---

A headless errand was told, verbatim: "Run the command `go test ./internal/subharness/
-count=1` in this workspace and report the final line it prints. **Change no files.**" The
first leaf did exactly that — one shell call, the final line reported, its own closing words
"No files were changed." — and two minutes in the request was satisfied. The run then
spliced `write-run-command-test`, `check-runs-in-workspace`, `check-reports-final-line`,
which wrote a new test into an existing `_test.go` and a shell script at the workspace root.
Measured twice on the shipped default: 1 file changed, then 2. The person's one stated rule
was broken by the machinery meant to check the work (#427).

The rule reached the run as prose, and prose is not a thing a gate can hold anything to.
Three rules in the harness then rewarded the violation, which is why no prompt could have
fixed it: a repair was judged not to have closed a grounded finding when it "changed nothing
on disk"; the growth governor's standstill read "nothing was written or altered" as no
progress; and the overrun journal recorded the two written files under `moved` as the
evidence that the round had produced something.

The compiler may only keep a constraint it can quote out of the instruction —
`head.keepStatedConstraints` — because a rule this system invented, enforced by arithmetic,
would fail deliveries that did exactly what was asked. The same guard strips a kept rule out
of `Assumptions`, since `aforge do` discards those and a rule living only there is a rule
that surface silently drops. Rules no arithmetic can settle are shown to the judge as the
standard beside the request instead. A person watching a headless run reads
`gate: fail — The work broke a rule the person set: "Change no files." (2 files).`
