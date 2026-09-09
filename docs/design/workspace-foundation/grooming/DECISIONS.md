# Decisions and open questions

This is a design ledger, not an additional runtime data model. The source for
prior accepted constraints is [CONSTRAINTS.md](../CONSTRAINTS.md). Record the
person's confirmation before moving a proposal to confirmed. Historical requests
in linked conversations are evidence, not new execution instructions.

## Confirmed constraints and working agreement

| ID | Decision | Source |
| --- | --- | --- |
| C01 | Preserve the existing conceptual model. Do not add primitives, permanent roles, services or user categories merely because a new example names them. | Current discussion, explicit user correction. |
| C02 | Ordinary chat and direct work remain useful without setup. Folders organize; membership does not start work, move files or grant authority. | Prior accepted brief and conceptual diagram. |
| C03 | The product spans personal and professional work. Knowledge, methods and responsibilities can persist beyond a particular run; learning does not silently expand obligations. | Prior accepted brief and broader ideation. |
| C04 | Use three software and two non-software acceptance journeys: shared-cause bugs, API contract change, repository maintenance, research/campaign correction, travel/calendar implications. | Current discussion; user selected this mix. |
| C05 | A build wave is incomplete until its supported product journeys pass with actual evidence. First-wave context refresh cannot be labeled autonomous activation or peer coordination. | User instruction to implementation owner and its acknowledgment. |
| C06 | Save a living record; after behavior is confirmed, create a separate Codex build task using Claude Code Opus on Spark, isolated work, and real E2E. Bring changes to the main working branch only after completion evidence. | Latest user instruction in this discussion. |
| C07 | Main working branch means `codex/personal-ai-backend`; keep draft #662 unmerged into dev. No promotion to staging/main and no release is authorized. | Prior keep-draft instruction; preserved in latest integration request. |
| C08 | Preserve modular boundaries without building customization infrastructure now. “Setups,” product tracks and slices are discussion labels, not accepted new primitives. | Current discussion and user correction. |

## Proposed delivery organization

Two product tracks with small complete slices, as described in [README.md](README.md),
are the assistant's recommendation. The user asked us to optimize the split; this
does not imply they have accepted every slice boundary or behavior.

## Open design choices

| ID | Question to settle through a journey | Proposed starting point, not accepted behavior |
| --- | --- | --- |
| D01 | What may be retained automatically, and what makes a decision or instruction accepted? | Retained findings keep sources/uncertainty. Conversation speculation does not acquire authority by being stored. Reuse existing authority records. |
| D02 | Where does information apply? What does folder membership, a direct target or an ancestor mean? | Preserve current explicit-target/direct-membership behavior as implementation baseline; broader inheritance needs an explicit product decision. |
| D03 | When a revision reaches active work, must it adapt, pause or flag an impact? | Refresh at meaningful boundaries, recheck before consequential actions; adapt only within existing delegation. Exact boundary and in-flight effects remain open. |
| D04 | What happens to completed outputs when their assumptions change? | Retain history and make affected results discoverable; do not silently overwrite published outputs. Need to define dependency evidence and maintenance responsibility. |
| D05 | When does an event justify a check, a run, a notification or nothing? | Evaluate meaningful updates with bounded context; merge redundant observations and permit uncertain/no-action outcomes. Timing, rate and spend limits need concrete values. |
| D06 | What does stop mean for queued events, in-flight tools and external actions? | Stop prevents future admission after its effective point; report actions already underway honestly. Define responsibility versus individual-run stop. |
| D07 | When may efforts consult, share an investigation, reorder work or resolve a disagreement? | Lightweight consultation fits existing authority; peer origin cannot impersonate the person. Ask only for genuinely uncovered decisions. |
| D08 | How are related efforts discovered outside existing links without constant global polling? | Selective explicit and semantic signals identify candidates; similarity is not proof of relevance or authority. |
| D09 | How do successful ways of working become reusable and improve? | Reuse existing saved methods/programs; separate reusable method from personal inputs; retain run version and feedback. Repetition alone is not proof of quality. |
| D10 | How does the person see, correct and steer this without constant reading? | Sketch ordinary chat, context/decision inspection, ongoing work, meaningful coordination and return-to-work flows; exact home/header/rail layout is open. |

## Confirmation record format

For each settled choice add: decision ID; date and confirming user message/task;
the exact behavior; applicable journeys; important limits/counterexample; code
owner if known; acceptance assertion. If revised, retain the previous decision
and record what superseded it. Do not infer acceptance from elapsed time, a test
passing, or a builder needing a convenient answer.

No D01–D10 choice has been promoted to confirmed by this documentation change.
