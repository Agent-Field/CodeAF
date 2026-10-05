# Contextual memory terminal acceptance

Run this on Spark with the built `bin/codeaf`, an actual configured provider,
and ordinary hosted `chat`. The recorder uses a persistent fleet terminal. It
never inserts memory, resumes old transcripts, or decides that fluent text is a
pass. Evidence must be outside the workspace the agent can inspect.

Build with `make build` from the repository root. Use
`scripts/contextual-memory-terminal.py start --evidence <outside-workspace-dir>
--workspace <project> --binary <repo>/bin/codeaf --state <clean-state-dir>`.
The state directory starts empty; the fleet provider environment supplies the
normal credential without copying its value. Finish first-run setup using the
terminal. `say --evidence <dir> --text <ordinary request>` records a user turn;
`capture --evidence <dir>` saves readable terminal frames alongside the ANSI
recording. `stop` interrupts the surface without killing its fleet shell.

A clean state directory is required for the initial conversation. Later fresh
conversations must retain that state, but start through the normal `/new` door
rather than resume. Capture conversation identities and inspect persisted
transcripts to prove the subsequent conversation has no old turn history.
Profile configuration is allowed; memory rows and answer-bearing workspace
fixtures are not. Keep expectations and test inputs outside the workspace.

Judge actual commands, edits, observed outcomes, and authorized interventions.
Assistant claims alone do not establish constraint compliance or successful
work. Keep the tested revision and binary SHA, recordings, all user inputs,
conversation identifiers, elapsed time, and the product spending receipts.
Changed-head evidence is historical diagnostic evidence, never final acceptance.
The recorder records the working tree dirty flag so that a source SHA cannot
silently stand in for an uncommitted snapshot.

## Required cases and honest accounting

For each case, record pass, fail, or not run with supporting tool/file evidence:

- A conditional constraint changes actions in a fresh conversation.
- A correction supersedes it within its original applicability.
- Rationale prevents repeating a rejected approach.
- An observed failed attempt guides matching work; changed conditions allow reconsideration.
- A real consumer dependency yields an actionable impact after a producer change.
- A similarly named unrelated project receives no warning.
- A deferred commitment becomes relevant when its prerequisite changes.
- A changed revision prevents stale evidence use.
- A dismissed consequence stays quiet until material new evidence.
- Completed or abandoned work stops resurfacing.
- Approved rules are present before relevant actions.

At least one useful task must finish. A controlled utility project is suitable
for supplemental diagnostics, but does not prove cross-project product behavior
by itself. A missing provider, skipped case, or old binary is not a pass. Match
memory-disabled runs where useful; do not add a second harness store or scheduler.

## Baseline diagnostic begun 2026-10-05

The initial source was `29245551f`; binary SHA256
`195901c3d1e8497e2f92813369f7e6844ffded538a89eca6fa573e3e50a5af26`.
The ordinary hosted terminal used the default DeepSeek V4 Flash Latest provider
configuration supplied by fleet, with an initially clean retained state.
The useful task was a spending ledger utility: exact decimal arithmetic, blank
row handling, and line-numbered malformed input errors. A natural request said
that release runtime must be offline and standard-library-only, while allowing
network use during development. The workspace contained only the flawed utility,
not expected responses or memory. This run is baseline evidence, not a pass for
the final implementation. See the task result for actual completion and failures.
