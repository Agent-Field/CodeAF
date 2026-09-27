This historical archive retains the full original cast, safe model audit, timeline,
text captures and independent outcome verification. Derivative GIFs and the
generated program are published only for the final accepted workflow.

# Adaptive reimbursement workflow — second live discovery

This is the continuous 215.418-second live recording of the actual reimbursement
workflow on Spark, application source `d44b8b7de5d6733e80a39bcbc69e39f00a3bde5e`.
The steering-marker defect was fixed. This run discovered a second UI defect:
automatically delegated read tasks displayed their entire briefs twice in the
conversation (see `duplicate-brief.gif`, first observed at 25.040 seconds).
This is finding evidence, not a claim that this revision passed clean UX acceptance.

The operator supplied a practical goal and then inspected actual results before
each follow-up. The model initially produced JSON despite the CSV steering, so
the next prompt corrected the actual missing deliverable. The model added --csv,
generated an importable category summary, updated the handoff, and grew the test
suite to 24 passing tests. Inspection found two handoff contradictions, which
were corrected in the same conversation. The model independently retrieved its
background task's HANDOFF content through its own tools; the recorder did not
copy, symlink, merge, or inject any workspace artifacts after launch.

The final handoff distinguishes the 160.00 subtotal for four valid rows from a
complete reimbursement: three invalid source rows remain unresolved and were
explicitly excluded with warnings. The original source checksum was unchanged.
The model re-imported the CSV and verified all figures. A separate fleet job
also verified 24 tests, CSV rows and sum, three warnings/exit 2, the saved output,
source bytes against Git, and the corrected handoff (`independent-verification.json`).

- `full-session.cast`: original real tmux framebuffer sampled at 4Hz, unchanged timing.
- `excerpt-180s.gif`: contiguous 1x-speed 180-second interval; exact bounds in
  `excerpt.json`. No removed time inside the interval or fabricated frames.
- `timeline.json`: actual operator prompts, steering, and snapshots.
- `models.json`: 81 completed model calls plus every start record; all use
  `deepseek/deepseek-v4.1-flash` through OpenRouter. Only safe metadata is included.
- `generated-project/`: the actual generated CLI, tests, CSV, reports, and handoff.

Fleet build/live job: `20260927-163649-001231-pr1607-adaptive-live-fixed` (exit 0).
Independent outcome verification: `20260927-164225-001233-pr1607-adaptive-outcome` (exit 0).
All execution occurred on Spark.
