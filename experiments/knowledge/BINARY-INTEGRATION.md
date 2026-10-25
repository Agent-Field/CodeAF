# Memory and connected knowledge in the experimental binary

Checkpoint: `77fba969c1f29066f34cb3cd4826e89e8199a141`.
This document describes the requested next integration, not completed behavior.

## User experience

Keep the top-level tab named **Memory**. Offer **Knowledge** and **Saved memory**
within it. A new profile has no knowledge capture until the person chooses
**Create knowledge base**. Explain that this starts collecting future chats;
importing old conversations is a separate explicit action.

Knowledge uses a folder/topic tree beside a readable article. Inline topic
references open canonical pages; the person can return through navigation
history, inspect incoming/outgoing links, search, and read source quotations.
The tree must adapt to narrow terminals. Keep normal place navigation and avoid
stealing printable characters from search. Show empty, pending, updating, failed,
and ready states honestly. Existing saved-memory controls remain reachable.

## Why keep both

Saved memory already admits short scoped preferences, explicit pins and binding
rules to chat context. Knowledge stores subject explanations, relationships,
open questions, disagreements and source evidence. A useful article is not
automatically a standing instruction. Preserve both stores and their semantics;
retrieve knowledge when relevant rather than inject the whole vault each turn.

Currently the experiment is not connected to binary capture or chat retrieval.
Do not describe the browser projection as installed agent memory.

## Implementation constraints

Reuse the tested Python compiler through an embedded, versioned runtime adapter;
do not require the source checkout at runtime. Check Python 3 availability.
Capture completed durable journal records with stable source IDs and recoverable
cursors. Human statements and agent synthesis retain different provenance.
In `userMessage`, `authored` means session-authored, not human-authored. Never
capture synthetic digests as words the person typed.

Scope every vault, capture and retrieval by authenticated profile/project.
Do not merge concurrent unrelated projects or expose local knowledge under a
remote host's identity. Keep network and compilation work off the TUI update
loop and agent mutex. Persist queued data before attempting model work.

Offer retrieval-only search/read tools on both chat tool belts. Return bounded
page text and source references; let the current chat model formulate the
answer. An additional model answer-generation request is unnecessary.

## Scheduling and cost

Queue committed content locally with zero model requests. Combine eligible
events from the same partition after 30 seconds quiet or 120 seconds maximum
age, bounded by the compiler's source/context caps. Process remaining events on
later passes. Unchanged content requires zero calls. New content gets bounded
extraction and reconciliation; full guide rewrites and curiosity maintenance
remain explicit until their runtime budgets are demonstrated.

All live model roles must use OpenRouter `deepseek/deepseek-v4.1-flash` and verify
the returned model. Credentials stay in the existing private credential path or
child environment, never command arguments, logs or source. Record extraction,
composition, retries and failures separately. A local link/search action costs
zero model requests. Do not use the OpenRouter Batch API.

## Acceptance and rollout

Additional proposed test interfaces: Memory actions to vault creation and linked
navigation; committed conversations to incremental capture and cited retrieval.
Earlier approved extraction/recall contracts continue to apply.

Validate no capture before opt-in, durable retry without duplicate publication,
project isolation, self/ambiguous/missing-link handling, keyboard navigation,
and retention of ordinary saved memory. Verify actual live TUI plus chat
retrieval with the pinned model, record usage, and review against the checkpoint.
Build a separate experimental binary on Spark; preserve the installed default.
Keep recordings and generated knowledge outside source control.
