# Prompts, tools and context: implementation contract

This complements the product plan. These are design requirements, not claims that the current branch implements them. The goal is capable autonomous workers with low coordination overhead and reliable contextual continuity.

## Prompt structure

Use a small stable shared policy, a foreground/task role policy, tool schemas generated from actual capabilities, and dynamic work context. Do not repeat the whole policy in each task brief. Keep volatile status out of the cacheable policy prefix. Do not put machine time, live spend, all-task state or changing captions into that prefix.

The foreground policy should express:

> Help the person think, decide and maintain continuity. Answer discussion and quick grounded questions here. Start sustained work using the relevant discussion and stay available. A request to implement starts work; exploring an idea does not silently change existing work. Address explicit changes to the relevant work. Bring back the usable result and material limitations. Do not supervise every worker step or ask for internal task-management decisions.

The worker policy should express:

> Own the assigned outcome. Use current user constraints and relevant evidence. Choose and execute the next useful work; delegate independent work when it will help. Integrate what you own. Resolve questions from available context; ask the parent when a consequential ambiguity cannot be resolved. Apply new user directions to the current assignment. Stop working when the requested outcome is reached, or report the specific unmet requirement when it cannot be reached. Return the answer/artifacts and meaningful checks, not a claim of being done.

A worker is not told both “nobody can answer you” and “ask your parent.” Neither prompt requires a fixed prewritten plan for every task. A plan is a revisable aid when useful. Neither prompt mandates research until certainty or a blanket full-suite review for tiny work. Model thinking is not published as progress. A caption describes current work and is not promoted to a factual conclusion.

Generate tool/default/limit descriptions from actual runtime constants. Keep model-facing terminology consistent with actual APIs and the manual. A capability absent at runtime is not promised by the prompt.

## What goes where

| Information | Foreground | Task owner | Child | Durable storage |
| --- | --- | --- | --- | --- |
| Person's current message | Full | Full if addressed to it; relevant quotation otherwise | Scoped applicable direction | Original with stable ID |
| Overall aim | Current relevant understanding | Purpose plus its local responsibility | Brief purpose; local assignment is scope | Current assignment revision |
| Binding constraints | Relevant current choices | Applicable set | Applicable subset | Source, scope, revision |
| Exploratory ideas | Conversation | Only when useful, explicitly exploratory | Normally absent | Original conversation |
| Tool evidence | Selected if useful | Relevant working evidence | Required input and handles | Full result + source/version |
| Other workers' findings | Material developments only | Dependency results and changes | Relevant dependencies only | Full result objects |
| Sibling transcript | On demand | On demand | Normally absent | Producer's thread |
| Progress captions | Display | Display | Display | Diagnostic/display events, not facts |
| Result preview | Display | Display | Display | Derived from full result |
| Full result | Available by handle | Its own + needed dependencies | Needed inputs | Bounded body, overflow reference, checks and delivery |
| Budget | User-relevant total when known | Remaining family and local allowance | Local allowance within family | Exact shared ledger |

No task receives the entire conversation state card. No child inherits an obligation to complete every word of the ancestor's multi-part request. Original words stay accessible as source; local ownership and applicable global constraints remain distinct.

## Handoff

Task admission is one operation, regardless of whether it comes from a typed command, model delegation, division, automatic handoff, continuation or resume. It records the current assignment, source message IDs, relevant inputs, tools/effects policy and budget. The same compiler constructs context at all doors.

Prefer explicit decisions, results and source-linked observations. Where legacy messages are used, label them as quoted conversation, preserving uncertainty and speaker. Never turn the first sentence following a tool call into something “already established.” A model may read the source when a quote is stale, incomplete or ambiguous.

Before a child starts: retain current constraints, exact input references, dependency results, known failed attempts worth avoiding, and the question it owns. Exclude unrelated conversation, raw sibling logs and repetitive narration. Select by task relevance and recency with one overall size budget; deterministic extraction is preferable to paying another model merely to restate an already sufficient brief. A model may retrieve more detail on demand.

## Tool access by responsibility

| Tool family | Foreground | Working task | Waiting task | Finalization |
| --- | --- | --- | --- | --- |
| Read/search/inspect | Available for useful grounding | Available within relevant access scope | Available on a meaningful wake, no polling loop | Relevant evidence/check inputs |
| Write/edit/execute | Short bounded operations only; sustained execution delegated | Available according to assignment's effects/ownership | No new effects while waiting for an essential answer | Only completion/check/authorized delivery effects |
| Delegate | Available | Available under family budget and cycle/ownership rules | Not used to evade an essential blocker | No new unrelated work |
| Ask/respond/steer | Address relevant work | Parent/children and direct user input | Reliable correlated answer delivery | Revision invalidates stale acceptance |
| Result/evidence retrieval | Available by work handle | Own and relevant dependency sources | Available without model polling | Full result and exact checks |
| External accounts/media/etc. | Only configured capabilities | Only configured and appropriately scoped capabilities | No speculative access | Same authorization as actual effect |

Use capability presence and effect permissions, not hundreds of brittle prompt prohibitions. Do not dynamically churn the tool list on every step; stable capability profiles plus runtime effect checks preserve caching and clarity. A changed profile becomes effective at a request boundary. Denied operations return an actionable structured reason and permitted route, not a vague failure that triggers endless retries.

Parent delegation cannot amplify permissions. A child cannot gain unrelated credentials or broader filesystem effects simply because it is a child. The original user may grant broader scope explicitly. Read-only research should not inherit unnecessary mutation machinery or Git lifecycle requirements.

## Tool-result retention

Keep the full original tool result durably with call ID, tool, canonical arguments, status, source revision where available, and artifact pointers. The model-facing view can be bounded. If truncated, say what is omitted and how to fetch a range or full result. Preserve errors, small decisive outputs, identifiers, paths, and useful counts. Separate stdout/stderr and exit status for commands. Do not infer success from a positive-sounding output string.

Large outputs should be paged or queried; handing all bytes to the model and later paying to summarize them is usually wasteful. A repeated call after changed inputs can be valid. Cache only with a sound input/version identity and invalidate on relevant effects. A build after an edit must not be flagged as unnecessary repetition.

## Compaction

Compaction changes the working view, never the authoritative assignment or result. Protect current user constraints, unhandled steering, pending questions, active dependencies, result references and the recent working exchange. Retain enough failed-approach/evidence context to avoid uncaused rediscovery. Preserve uncertainty.

Retire bulky consumed tool bodies before valuable decisions. Keep an explicit pointer and factual metadata. Do not treat tool output as consumed solely because a write occurred elsewhere. Preserve assistant text accompanying calls, multi-call causal order and error results. Never duplicate journal replay as fresh execution.

Use deterministic reduction first. Use a model-produced checkpoint only when genuinely useful; label it as a fallible working summary and preserve source references. Before accepting a model summary, mechanically verify required constraint/question/result IDs were retained. This checks coverage, not semantic truth. A valid next request must still fit with tool schemas and reserved response capacity. Test real small and medium windows; target arithmetic alone is insufficient.

On resume, rebuild from authoritative current records plus relevant working context. If the last mutation may have completed, reconcile effects before replay. Display summaries are not recovery state.

## Completion and checking

The worker produces a full result and states the current assignment revision it satisfies. The runtime distinguishes completion from cancellation, budget exhaustion and waiting. Do not encode completion as a stop-string routed through a threshold failure path. Natural final responses may complete a simple task without another ceremonial model turn; an explicit completion action, if used, must share that exact finalization path.

Before final delivery: resolve owned children, evaluate applicable checks, confirm result revision and target artifact preconditions, and persist delivery receipt. Checking is proportional to the task. Prefer deterministic checks for deterministic requirements. Qualitative judgment can use one targeted independent review or the owner’s integration judgment when it adds information; avoid a universal chain of critics.

Preserve full results before rendering short cards. Dependency input, continuation and main-chat foldback use full relevant result/evidence records, not card text. Repair only unmet requirements and reuse valid work. A changed instruction during checking invalidates the stale acceptance result.

## Acceptance cases

- A command appearing after three introductory lines survives producer → owner → main → next child → restart.
- A child receives its scope and the latest user correction through every admission door.
- A progress caption is never treated as an established finding.
- A 21st correction survives a context budget while redundant older facts are dropped.
- Text plus a new tool call preserves the preceding result's interpretation.
- Large tool output remains fetchable after compaction, and errors retain actionable detail.
- A question asked after a child join is delivered and answered exactly once.
- Tool permissions cannot broaden through delegation; a legitimate task has the tools it needs.
- A task completes with the same semantics with review on, off or inconclusive.
- Small research, coding, writing and data tasks avoid unnecessary naming/planning/reviewer model rounds.
