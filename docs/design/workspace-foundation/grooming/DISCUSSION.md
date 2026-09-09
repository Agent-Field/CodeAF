# Discussion map for the system diagram

Checkpoint: 2026-09-09. Numbers match [SYSTEM.svg](SYSTEM.svg). They are anchors
for discussing the existing design, not new primitives, a sequence of phases,
or separate implementation teams. Accepted behavior remains in [DECISIONS.md](DECISIONS.md).

## Where we are

The conceptual direction is agreed. Collections/membership, existing owner
resolution, sourced information with revision/withdrawal, context on explicit
resumption and ordinary workers have bounded functional evidence. The fresh
three-consumer API case passed at `12017ee2a`.

The expanded workday exposed existence being confused with applicability after
membership removal. The owner's final handoff is `ca2a59d5a`, with scope repair
`c43119be5` and receipt-display clarification `3a6140a72`. It reports the forced
identity-read test passing against unchanged production `3a6140a72`; scope
removal/rejoin/withdrawal, large retrieval and conflicting-source cases now have
passing live evidence. Full deterministic regression also passed. The complete
workday remains RED: completion challenged a correct numeric revision and the
agent changed it to a string, failing the artifact contract (issue #672).
Ordinary final CI was pending in that report; paid CI has no secret. Preserve
the original failures. See [DEMONSTRATION.md](DEMONSTRATION.md).

Later checkpoint: the implementation owner pushed completion repair `fa9f1fe64`.
Its focused real-reader checks accept a correct report and reject a wrong revision
type, wrong source and failed write. Full repaired complex journeys and affected
suites are still running. This does not yet clear the complete-workday failure.

The complete agreed model is not implemented merely because records exist.
Accepted-decision semantics, general scope, activation with new context,
cross-chat collaboration, semantic discovery and learning/adoption remain open
or only partly integrated. The diagram is logical architecture, not a shipped
capability matrix or proposed deployment topology.

## Numbered areas

### Return-to-diagram checkpoint after C15

These are qualitative progress labels, not percentages. "Agreed" concerns product
behavior; "evidence" concerns what the implementation has actually demonstrated.

| Area | What is clear | What keeps the area open |
| --- | --- | --- |
| 1 — Folders | C12/C15: start anywhere, automatic reversible discovery, a stable way back, correcting associations without deleting content. | Explicit folder/subtree rule behavior, actual moves versus references, correction persistence and concrete retrieval quality. |
| 2 — Decisions and memory | C09/C15: no extra save for explicit decisions, retain source and understood scope, explicit trip direction survives unlinking. | Exceptions/conflicts, moving work, inferred context versus accepted direction, forgetting and memory-off semantics. |
| 3 — Current context | Explicit targets, revision/withdrawal and resumed consumers have bounded live evidence. | Discover unlinked relevance; identify affected outputs; define response of running, idle and completed work. |
| 4 — Activation | Direct requests, schedules/events and retained work have places in the model. | Connect current decisions to real activation; duplicates/restarts, quiet checks, budgets and stop semantics. |
| 5 — Cooperation | Existing executions can perform parallel work; sourced information has explicit consumers. | Bounded live cross-chat consultation, disagreement, ownership and shared work under retry/restart. |
| 6 — Actions and outputs | Tools perform work; artifacts and sources persist; C15 distinguishes a current artifact from historical messages. | Actual connector effects, uncertain outcomes, global artifact identity, reuse/adoption and end-to-end proof. |
| 7 — Human view/control | Find, inspect, correct and stop are required; current output and original discussion serve distinct return intentions. | Concrete TUI behavior for arrival, change, scope, history and correction; demonstrate usability through the real surface. |

The model accommodates the intended direct-work, parallel-team and ongoing-helper
patterns using existing concepts. This does not establish a practical superset of
Claude Code, OpenClaw or Grok Bot. All five complete journeys in JOURNEYS.md remain
unproved as wholes: a context-seam pass is not a working software/marketing factory
or an autonomous calendar assistant. The next cross-area demonstration should carry
one agreed correction from retained direction through an affected run to a current
output and a visible explanation, including an unrelated negative case. Activation,
peer consultation and actual effects then need evidence at their real boundaries.

| Diagram number | Discussion | Preserve | Questions to resolve |
| --- | --- | --- | --- |
| 1 — Folders | Organization and scope | Folders organize. Links, relevance and authority differ. Ordinary chat needs no setup. | What belongs versus references? What does current folder mean for a multiply linked chat? Do accepted rules extend to children, with what exceptions? What changes when something moves, joins or leaves? |
| 2 — Decisions, alongside memory | Retained meaning | Explicit person decisions need no extra save step (C09). Learned context does not silently grant permission or establish obligations. | How do we distinguish accepted direction, inference, quotation and peer advice? How are source, scope, correction, conflict and forgetting represented? Which essentials survive with optional memory off? |
| 3 — Select relevant state | Context and change propagation | Current information can be accessible without applying here. Select useful context instead of injecting all history. | How do we discover unlinked relevance and choose bounded context? What depends on a changed premise? What happens to running, idle and completed work? When must it adapt, pause, or report impact? |
| 4 — Activation, backed by work | Responsibility and lifecycle | Finite/ongoing work differs from an individual execution. Authorization persists across runs within its scope. | What establishes a responsibility? Which schedules/events/changes justify a run or nothing? What survives closed terminals/restarts? How do duplicate observations, uncertain results, overlapping runs, spend limits and stop work? |
| 5 — Agent execution | Cooperation | Communication is not person authorization. Permanent roles/managers are optional. | When consult existing work versus spawn new work? How find and address its current owner? What context travels? How handle disagreement, shared resources, ownership, bounded replies and duplicated effort? |
| 6 — Tools and return path | Effects, outputs and reusable methods | Real tool access determines capability. Artifacts and useful methods can outlive a run. Reuse existing capabilities and boundaries. | What proves an external action happened? How recover from an uncertain result? What records artifact origin/version/dependencies? How reuse and improve methods without silently expanding work? Which integration/hosting gaps block an actual journey? |
| 7 — Chats and work views | Human understanding and control | One underlying state can support multiple views. No mandatory organization ceremony or machinery-heavy display. | What is visible on arrival, during execution and after absence? How inspect sources/scope, discover a stale result, correct direction, pause or stop? What merits attention? How can future customized views inspect/control the same state without creating a second account of it? |

Mapping to existing open items: 1→D02; 2→D01/D02; 3→D03/D04/D08;
4→D05/D06; 5→D07/D08; 6→D04/D06/D09; 7→D10 plus the human controls
for each preceding behavior. This reorganizes the existing questions and does
not convert proposals into decisions.

## Where to begin in discussion

The user's C12 clarification changes the starting point: first explore entering
an interaction from anywhere, continuing known work, and starting in a location
that later proves unrelated. A folder is a predictable place to return and a
focus signal, not a mandatory routing choice. Organization may follow emerging
understanding, but automatic placement/moves, references, topic drift and their
visibility remain proposals. No new inbox/project/setup primitive is implied.

Discuss 1 and 2 together: what a statement means and where it applies. Sketch
the corresponding acknowledgment/inspection/correction in 7 at the same time.
The implementation owner can continue testing the existing part of 3 while we
do that. Broaden to activation and collaboration when the next concrete
experiment requires them, rather than trying to finalize all seven in advance.

For expectation-dependent choices, C14 makes the next discussion concrete before
asking the person to decide: present a situation, synthetic expectations and
counterexamples, viable alternatives and a reasoned recommendation. Open prompts
explore expectations; option prompts compare stated behaviors and must be labeled
as such. A clear reply settles the stated behavior; uncertainty leaves it open.
Do not repeat a paid probe for unchanged questions or turn simulation into another
mandatory phase. Keep settled/open/next checkpoints short. The second probe tests
automatic discovery, correcting a wrong association and trip-specific scope.

Across every area, assess completed outcomes, human explanation/correction,
inconsistent assumptions, recovery effort, latency and cost. Modularity means
preserving useful boundaries, not building an add-on platform now. Software and
nonsoftware journeys are counterexamples to the same rules.
