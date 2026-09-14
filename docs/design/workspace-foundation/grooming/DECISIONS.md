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
| C16 | Explicit exceptions change only their intended conflicting guidance; compatible instructions remain. Reprocessing unchanged history must not restore a corrected inferred association; later explicit, purpose-specific reuse can establish new relevance without rewriting the original meaning. Rules explicitly tied to a folder apply while work belongs there: an actual move changes guidance for future work, preserving existing drafts/history and item-specific direction. A reference alone is not a move. | 2026-09-09: after the round-3 scope findings and current-folder recommendation, user replied “okay this is good” and requested the next decisions. This confirms those product rules, not a universal newest-wins priority algorithm or every memory/activation behavior. |
| C17 | Preserve the goal of covering the useful complete tasks of Claude Code, OpenClaw, Grok Bot and similar systems, and enabling additional useful combinations with less setup, repetition, inconsistent assumptions and steering effort. Check real task coverage and remaining gaps under actual tools, access, budgets and hosting. | User repeatedly states the superset-and-more goal, most recently “make sure ... everything ... others ... doable and more ... with this.” This is a goal and evaluation requirement; it is not evidence of achieved containment or of an exclusive capability no programmable competitor could implement. |
| C18 | Assess whether a simulation makes sense and has reached a stable conditional explanation, not merely whether models selected similar answers. Inspect concise rationales against supplied facts, paired-wording consistency, counterexamples and changed circumstances. Distinguish defensible product alternatives from model errors; report unresolved choices instead of sampling until agreement. | 2026-09-09 steering during round 4: user asked to check convergence and improve reasoning rather than just selection, then asked to continue. This strengthens C14; it does not make synthetic reasoning a substitute for real product E2E or human use. |

## Confirmed implementation direction — 2026-09-10

- C19 / D11: the user accepted the recommendation for typed backend entities,
  composable versioned components, simple properties and typed relationships;
  backend types remain independent of user-facing labels. Source: “yes go with
  your recomendation and lets start building them with subagents and parallalize
  when you can ... keep updating checklist.” This settles composition, not every
  other open architecture decision.
- C20: start an isolated integration candidate retaining the backend draft and
  integrating current dev; preserve existing drafts. Parallel implementation and
  verification subagents are explicitly authorized. Candidate validation remains
  distinct from parent integration and merging into dev.
- C21: prioritize a working functional backend; defer tui3 tests and similarly
  expensive broad UI/E2E runs for now. Source: “dont do tui3 tests etc.. as they
  are very compute intensive lets get a working functional system first.” Use
  focused functional checks on Spark and record broader acceptance as deferred.
  This supersedes the broad-test portion of earlier delivery instructions for
  this iteration; it does not turn unrun acceptance into passing evidence.

## Proposed delivery organization

The earlier fixed sequence of tracks and slices was an assistant proposal. C11
supersedes it as the active working method. [The demonstration](DEMONSTRATION.md)
records the current experiment, evidence boundary and first missing transition.
Existing isolated-build and integration safeguards still apply.

## Open design choices

| ID | Question to settle through a journey | Proposed starting point, not accepted behavior |
| --- | --- | --- |
| D01 | What else may be retained automatically, and how is explicit person acceptance distinguished from speculation, quoted text or peer suggestions? | C09 settles no-extra-save retention for explicit person decisions. It does not settle automatic promotion of inferred findings or a new authority mechanism. Reuse existing authority records. |
| D02 | Where does information apply, and how are remaining conflicts or ambiguous scope resolved? | C12/C15/C16 settle the fallible starting location, explicit item scope, exceptions preserving compatible guidance, and current-folder rules following actual membership for future work. Discovery/reference alone does not expand applicability; moves preserve prior drafts/history and item-specific direction. General conflict adjudication, unclear intent, multiple governing memberships and implementation remain. Current bounded implementation uses explicit targets/direct membership; the agreed wider semantics are not yet proved. |
| D03 | When a revision reaches active work, must it adapt, pause or flag an impact? | Refresh at meaningful boundaries, recheck before consequential actions; adapt only within existing delegation. Exact boundary and in-flight effects remain open. |
| D04 | What happens to completed outputs when their assumptions change? | Retain history and make affected results discoverable; do not silently overwrite published outputs. Need to define dependency evidence and maintenance responsibility. |
| D05 | When does an event justify a check, a run, a notification or nothing? | Evaluate meaningful updates with bounded context; merge redundant observations and permit uncertain/no-action outcomes. Timing, rate and spend limits need concrete values. |
| D06 | What does stop mean for queued events, in-flight tools and external actions? | Stop prevents future admission after its effective point; report actions already underway honestly. Define responsibility versus individual-run stop. |
| D07 | When may efforts consult, share an investigation, reorder work or resolve a disagreement? | Lightweight consultation fits existing authority; peer origin cannot impersonate the person. Ask only for genuinely uncovered decisions. |
| D08 | How are related efforts discovered outside existing links without constant global polling? | Selective explicit and semantic signals identify candidates; similarity is not proof of relevance or authority. |
| D09 | How do successful ways of working become reusable and improve? | Reuse existing saved methods/programs; separate reusable method from personal inputs; retain run version and feedback. Repetition alone is not proof of quality. |
| D10 | How does the person see, correct and steer this without constant reading? | Sketch ordinary chat, context/decision inspection, ongoing work, meaningful coordination and return-to-work flows; exact home/header/rail layout is open. |

## Backend architecture decisions added after critical review

The following extend D01–D10 rather than replacing confirmed constraints. The user
asked on 2026-09-10 to settle architecture decisions and explicitly launch
subagents to compare retaining the draft with starting from dev. That authorizes
read-only baseline analysis; it does not confirm the alternatives below.

| ID | Decision | Recommendation, not yet confirmed |
| --- | --- | --- |
| D11 | Backend extension model | Typed entities, owned versioned components and values; avoid universal entity/property storage. UI labels do not determine backend types. |
| D12 | Persistent execution identity | Separate identity from profile, duty and process where a named role spans duties. Do not require identities for ordinary direct work. |
| D13 | Effective-input and lifecycle versions | Distinguish specification/control revisions, attempt identity, wait token and fence; retain the full effective-input manifest. |
| D14 | Occurrence, continuation and reporting policies | New recurrence versus resumed unfinished outcome is explicit; missed runs, overlap and reporting are configured behavior, not inferred from a generic trigger. |
| D15 | Dependency and ownership transfer | Dependencies track output versions; shared investigations and owner handoff have atomic claims and origin-aware messages. |
| D16 | Capability and execution-resource boundaries | Narrow effective permissions; explicit host/account/browser/worktree binding; copied profiles do not copy grants or private state. |
| D17 | Durable persistence and recovery | Preserve authoritative lifecycle owners, durable publication/admission and uncertain-effect reconciliation; justify any new coordinator store with concrete transactions. |
| D18 | Extensibility and compatibility | Typed versioned adapters declare authority, replay, cancellation and delivery contracts; unknown behavioral variants remain inactive and inspectable. |

[The working checklist](NEXT-STEPS.md) records discussion order and delivery progress.
Baseline choice is an implementation decision currently under review; C07 remains
in force until the user settles an alternative integration destination.

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

C15 settles the discovery/correction direction and the explicit trip
example. Acceptance should demonstrate that an edit through either entry point
reaches the same current artifact, old messages retain their original text, a
clear association correction preserves both artifact and conversation, and the
trip's budget survives unlinking without governing unrelated work. These are
confirmed product assertions, not claims of implemented semantic discovery or
global artifact identity. The remaining explicit folder rules, exceptions, moves
and persistent correction behavior are addressed by the subsequent C16 record.

C16 now settles the three round-3 product rules: the explicit conference exception
does not alter other trips, a rejected inference stays rejected on unchanged
rereading, and explicit current-folder scope follows actual membership for future
work. Acceptance must include a later explicit reference that does not merge the
trips, an actual move versus a reference, unchanged old drafts/history, and an
item-specific instruction surviving that move. Formal and concise are compatible;
an exception is not permission to discard unrelated applicable direction. Broader
conflict adjudication, retention/forgetting, and running/completed-work reactions
remain open. Before dispatch, DELIVERY.md still requires inspected reuse, a stable
integration base, shared-interface agreement and complete bounded E2E assertions.

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

## Draft maintenance — C22

2026-09-10: the user requested updating the existing draft with baseline details
and avoiding hanging open draft PRs. Consolidate design/checklist records into
#662, preserve their history, then close superseded grooming #663. Do not create
another implementation draft merely for the isolated candidate. Keep #662 draft
and unmerged into dev, and retain test deferrals visibly.

## Implementation choices in W5-B (2026-09-14) — not owner approvals

Made inside the already authorized slice ("truthful multiple-folder/single-report
handling"; reuse existing machinery; no new concepts). Each can be reversed by the owner.

- **I-W5B-1. A file watch is said by its pattern.** `When.CardWords` returns
  `when <glob> changes` for an unconditioned watch, and `when_words` are not recorded for
  one. This supersedes the chat door round-1 lane choice "the model's own words still
  win" for file watches only, because a card could promise a folder the pattern never
  reaches. Other kinds keep the model's words.
- **I-W5B-2. The report-on-edit gap is closed at the schema text**, not by a stop card: the
  live cause was the model's stated belief that a report path cannot be edited. Confirming
  a chat stop on a card is left as an open owner decision (it changes every stop).
- **I-W5B-3. Two sibling folders into one report stay unsupported** and are said to be: no
  brace expansion, no broadened pattern chosen for the person; the manual names the honest
  outcomes.

## Governing scope and causal records — C23–C25

2026-09-10, C23: the user accepted the recommended scope rules and authorized
implementation. An explicit item exception alters only conflicting guidance;
compatible budget, access and publication conditions remain. Folder guidance
reaches descendants only when explicitly included. A shortcut or discovered
association supplies relevance, not authority. A material unresolved conflict
requires one focused question; unaffected work may continue. This confirms the
behavior, not a universal newest-wins or nearest-folder-wins algorithm.

C24: the same instruction requests inspectable runtime cause/effect evidence for
product review: what started work, which scope and revisions it used, what changed,
and what actually happened. Reuse the existing execution journal, distinguish
observed operations from model explanations, and expose gaps rather than invent
causal links. This does not require storing private reasoning or a second audit
service. Source approvals distinguish the person, delegated principals and unknown
legacy provenance. A model-written accepted flag is not authority.

C25: the subsequent laptop-heat instruction requires ALL compilation and test
execution on Spark, including targeted checks and `make build`. Local work is
source editing, formatting and inspection only. The two stale local test-log
pollers were stopped; no Go build/test processes were found. Tui3 and broad
expensive acceptance remain deferred under C21.

C26: the user explicitly reaffirmed Claude Code with Opus on Spark for subsequent
implementation, instead of Codex implementation/subagent tokens. Codex coordinates,
checks results and maintains the existing draft/checklist; substantial coding and
analysis run through Claude Code Opus on Spark. Do not silently fall back to Codex
coding if Opus is slow or unavailable. Report an actual access blocker. Claude Code
availability and an authenticated Claude subscription were checked on Spark on
2026-09-10. C25 Spark-only compilation/testing and C21 expensive-test deferral remain.
