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
membership removal. The owner reports a repair pushed at `c43119be5`, passing
focused deterministic checks. A subsequent receipt-display clarification keeps
applicability visible when text is truncated. At this checkpoint the full
complex live tests, conflict case and regressions are NOT established green.
Keep the original failing evidence. See [DEMONSTRATION.md](DEMONSTRATION.md).

The complete agreed model is not implemented merely because records exist.
Accepted-decision semantics, general scope, activation with new context,
cross-chat collaboration, semantic discovery and learning/adoption remain open
or only partly integrated. The diagram is logical architecture, not a shipped
capability matrix or proposed deployment topology.

## Numbered areas

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

Discuss 1 and 2 together: what a statement means and where it applies. Sketch
the corresponding acknowledgment/inspection/correction in 7 at the same time.
The implementation owner can continue testing the existing part of 3 while we
do that. Broaden to activation and collaboration when the next concrete
experiment requires them, rather than trying to finalize all seven in advance.

Across every area, assess completed outcomes, human explanation/correction,
inconsistent assumptions, recovery effort, latency and cost. Modularity means
preserving useful boundaries, not building an add-on platform now. Software and
nonsoftware journeys are counterexamples to the same rules.
