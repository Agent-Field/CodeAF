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
| C09 | An explicit decision expressed by the person is retained with its source and understood scope without a separate save request. | 2026-09-09, current discussion: user confirmed the quoted automatic-retention proposal with “yes”. Scope inheritance is being clarified separately in D02. |
| C10 | Groom general rules and relationships first; use the domain journeys as consequences and counterexamples, not as reasons to build domain-specific architecture. | 2026-09-09, current discussion: user asked to work at the general level and let use cases emerge. |
| C11 | Evolve one system drawing and one working demonstration together; choose experiments by uncertainty resolved and use the five journeys as substitutions/counterexamples. Keep discussion alongside implementation of the last agreed behavior. | 2026-09-09: user rejected the multi-step process as inefficient, then accepted this revised approach with “okay lets go”. This confirms the working method, not open product semantics. |
| C12 | Folders support intuitive navigation and return: find expected chats/artifacts/work and continue or start something. They provide local focus with awareness of relevant work elsewhere. Starting location can be unspecified or mistaken; the person should not need to understand the whole tree or choose a deep folder before interacting. | Current 1/2 discussion: user described folders as places to return, stressed global context and warned that the starting folder may not be the relevant location. Automatic placement, moves, multiple homes and instruction-scope rules are still open. |
| C13 | Build a reusable short-response multi-model probe using the existing OpenRouter credential, around 100 samples, delegated alongside discussion, to explore broader expectations and concerns. | Current 1/2 discussion: user explicitly requested the script and subagent. The experiment is authorized; its synthetic outputs do not confirm product choices or constitute measured public opinion. See EXPECTATIONS.md. |
| C14 | For expectation-dependent grooming, simulate concrete situations before asking the person to settle behavior. Use open questions or explicit alternatives as appropriate; present the situation, model expectations and dissent, available choices and our recommendation. The person can accept or leave unclear behavior open. Always show what is settled and the next step toward completion, checking against the whole personal-AI goal. | 2026-09-09: user asked to apply simulations to these questions, choose together, retain the goal and report progress/next steps. This confirms the working method; model agreement is not product approval. |
| C15 | Relevant work can be discovered automatically through reversible associations. Editing the same current artifact through another folder updates that artifact, while historical conversation text preserves what was said. A clear correction of an inferred discovery association takes effect with a brief explanation and undo, preserving the artifact and mixed conversation. An explicit trip-specific instruction survives removing a discovery association and does not govern unrelated work. | 2026-09-09: after the round-2 situation/consequence table and recommendation, user replied “okay lets go” and requested the next scope simulation plus a whole-diagram checkpoint. This accepts those proposed behaviors; it does not settle general folder-rule inheritance, moves, negative correction persistence, automatic activation or the exact historical/current artifact presentation. |

## Proposed delivery organization

The earlier fixed sequence of tracks and slices was an assistant proposal. C11
supersedes it as the active working method. [The demonstration](DEMONSTRATION.md)
records the current experiment, evidence boundary and first missing transition.
Existing isolated-build and integration safeguards still apply.

## Open design choices

| ID | Question to settle through a journey | Proposed starting point, not accepted behavior |
| --- | --- | --- |
| D01 | What else may be retained automatically, and how is explicit person acceptance distinguished from speculation, quoted text or peer suggestions? | C09 settles no-extra-save retention for explicit person decisions. It does not settle automatic promotion of inferred findings or a new authority mechanism. Reuse existing authority records. |
| D02 | Where does information apply? What does folder membership, a direct target or an ancestor mean? | C12 makes the starting folder a potentially useful but fallible focus signal. The earlier proposal that an unqualified decision automatically governs the current folder is not accepted. Explicit folder/subtree scope remains a desired direction to clarify, while discovery/reference does not itself expand applicability. Interpret actual intent, including starts without a location or in the wrong location; resolve scope capture, exceptions, multiple memberships and later moves. Current implementation remains explicit targets/direct membership, not recursive inheritance. |
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

C09 partially settles D01. D02 records the user's scope direction and questions;
the proposed complete inheritance behavior is not marked confirmed.

C12 further clarifies D02/D10: the interaction can precede its organization. Do
not require choosing a folder first or treat the starting location as conclusive
evidence of the subject or scope. The user raised changing “ownership”; whether
that means filing/home, work responsibility or both needs clarification through
the experience, not an automatic change to runtime owner/authority semantics.

C15 settles the proposed discovery/correction direction and the explicit trip
example. Acceptance should demonstrate that an edit through either entry point
reaches the same current artifact, old messages retain their original text, a
clear association correction preserves both artifact and conversation, and the
trip's budget survives unlinking without governing unrelated work. These are
confirmed product assertions, not claims of implemented semantic discovery or
global artifact identity. D02 remains open for explicitly folder-scoped rules,
exceptions and moves; persistent correction behavior is being simulated next.

## C09 acceptance boundary

The source is the person's current statement, not text quoted from an artifact or
another agent. A future acceptance test should express an explicit decision in
ordinary chat without a save command, inspect the retained source and scope, and
verify the appropriate consumer can consult it. A speculative suggestion must
not become accepted direction by the same path. This is a product confirmation,
not a claim that the current informational `shared_context` implementation
already recognizes and enforces accepted decisions. A new build task waits for a
complete confirmed scope/authority contract rather than implementing this atom
in isolation.
