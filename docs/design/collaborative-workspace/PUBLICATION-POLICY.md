# Publication policy: branch-local ideation until verification

**Latest owner instruction · 19 September 2026.** Automatic continuation is authorized: after each verified receipt, kick the next wave immediately and give the owner an immutable binary to try. This supersedes earlier requests to publish GitHub issues, open a PR as work progresses, or pause between waves for an owner checkpoint.

- Keep all four waves, product/engineering design, journey checklists, PlanDB progress exports and receipts in files on `feat/collaborative-workspace-0918`.
- The branch may be pushed to origin. Do not create additional GitHub issues, reopen the closed ones, post issue updates, or create a PR during this unverified phase.
- The supervisor closed the four previously created issues (1216–1219) as not planned in GitHub because tracking is moving into the branch. Their closure is **not** completion or cancellation of the four implementation waves.
- Continue all four waves automatically after each wave’s verified receipt. The owner still wants a copy-paste Spark launch command per wave; do **not** pause for their try before starting the next wave.
- After Wave N `releases/wave-N/ready.json` is written and the PlanDB wave root is marked done, immediately decompose and run Wave N+1. Do not start a dependent wave before that receipt. No PR is authorized before owner verification of the whole feature.
- Earlier phrases such as “issue 1” in task descriptions or filenames mean the local Wave 1 work package; they do not instruct creation or reopening of GitHub issues. The canonical plan is the branch's serial-plan.md plus USER-JOURNEYS.md. Remove active GitHub issue links/status dependencies from those documents and use local wave-file links.
- Keep authentic test evidence and normal repository code/manual rules. Do not invent a PR number for a changelog entry. Record invalidated assumptions locally while no PR exists; add the actual required PR-number entry when a feature PR is eventually opened.

The latest owner sentence said “origin as branch, not issue or PR until we verify”; that is the publication boundary being followed. Preserve the full feature scope and test requirements.
