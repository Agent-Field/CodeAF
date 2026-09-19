# Publication policy: branch-local ideation until verification

**Latest owner instruction · 18 September 2026. This supersedes earlier requests to publish GitHub issues or open a PR as work progresses.**

- Keep all four waves, product/engineering design, journey checklists, PlanDB progress exports and receipts in files on `feat/collaborative-workspace-0918`.
- The branch may be pushed to origin. Do not create additional GitHub issues, reopen the closed ones, post issue updates, or create a PR during this unverified phase.
- The supervisor closed the four previously created issues (1216–1219) as not planned in GitHub because tracking is moving into the branch. Their closure is **not** completion or cancellation of the four implementation waves.
- Continue Wave 1 implementation using PlanDB and parallel Cursor workers. The owner wants to be told when Wave 1 is verified and given the exact working Spark binary command to try it.
- Hold Wave 2 at that owner test checkpoint. A single draft feature PR can be considered after verification; no PR is authorized by this workflow before that point.
- Earlier phrases such as “issue 1” in task descriptions or filenames mean the local Wave 1 work package; they do not instruct creation or reopening of GitHub issues. The canonical plan is the branch's serial-plan.md plus USER-JOURNEYS.md. Remove active GitHub issue links/status dependencies from those documents and use local wave-file links.
- Keep authentic test evidence and normal repository code/manual rules. Do not invent a PR number for a changelog entry. Record invalidated assumptions locally while no PR exists; add the actual required PR-number entry when a feature PR is eventually opened.

The latest owner sentence said “origin as branch, not issue or PR until we verify”; that is the publication boundary being followed. Preserve the full feature scope and test requirements.
