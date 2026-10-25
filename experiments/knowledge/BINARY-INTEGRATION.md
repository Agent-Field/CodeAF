# Memory and connected knowledge in the experimental binary

Checkpoint: `219588e` (inline links); previous complete workflow: `77fba96`.
This branch implements the local persisted-chat integration described below.

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

The experimental binary now captures opted-in local chats and exposes cited
knowledge tools. This does not install or enable knowledge in the default binary.

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

Approved test interfaces: Memory actions to vault creation and linked
navigation; committed conversations to incremental capture and cited retrieval.
Earlier approved extraction/recall contracts continue to apply.

Validate no capture before opt-in, durable retry without duplicate publication,
project isolation, self/ambiguous/missing-link handling, keyboard navigation,
and retention of ordinary saved memory. Verify actual live TUI plus chat
retrieval with the pinned model, record usage, and review against the checkpoint.
Build a separate experimental binary; preserve the installed default.
Keep recordings and generated knowledge outside source control.

## Implemented limits

The adapter needs Python 3 on Linux or macOS. Compilation runs while a persisted
local chat is active; pending journals recover when their chat reopens. There is
no always-on global service scanning dormant chats. New guide discovery is
bounded; existing guide prose is retained, with newly captured member claims
exposed under sourced **Recent knowledge** until editorial integration. This
avoids full-guide model rewrites on each turn. Superseded support is withheld.

Normal and bash-belt chats plus their session child workers can retrieve pages.
Standalone `codeaf do`, remote knowledge transport and the desktop are not wired
by this experiment. Hosted windows explicitly refuse local-vault fallback.
Tool responses are capped at 12,000 bytes; long reads provide versioned cursors.

Knowledge search/read make no extra model calls. Their small tool schemas remain
in the persisted chat's cached tool belt before opt-in so creation becomes usable
without restarting; those schema tokens are part of ordinary chat cost. The
metadata-only status poll does not hydrate articles. Imported sources are archived
by identity, indexed pending counts avoid rescanning their contents, and unchanged
projections skip export. Vault JSON hydration and lexical search still grow with
collection size; large-scale throughput and long-running quality remain work.

## Reader polish checkpoint

The reader uses the chat UI's underlined link treatment for `[[topic]]` references,
with the same canonical target for mouse clicks and the Ctrl+L link list. Tree
selection uses the shared cursor ground, with a textual marker on terminals that
cannot paint it. Leaf rows omit decorative diamonds. Paragraphs wrap at at most
100 cells, while narrow terminals keep the existing tree/article navigation.

Sources are a compact clickable disclosure (Ctrl+O), grouped by original source,
with readable speaker/date and quotations. Return restores the article's reading
position; source identities remain in the vault rather than repeated in prose.
The reader changes require no extraction, reconciliation, or model requests;
incremental model cost for this UI iteration is $0. Infrastructure and ordinary
chat/extraction costs are separate.

Snapshot adoption builds page and connection indexes. Steady frames format only
visible tree rows; hover reads the displayed frame's hit map. A synthetic
120×40-terminal benchmark with short articles and one edge per page measured
about 55 µs/frame at 1,000 pages and 46 µs at 10,000 pages (sampling noise; neither
includes terminal/network delivery). All-match filtering measured 0.49 ms and
4.82 ms respectively. Filtering and snapshot hydration still grow with corpus
size, and dense links or long articles need separate measurements. These are
layout measurements, not end-to-end SSH latency guarantees. Remote `--host`
knowledge transport remains unsupported; ordinary SSH runs the vault reader on
the machine where the binary executes.

## Sidebar interaction follow-up

The wheel uses its pointer coordinates: over the sidebar it advances the tree
viewport immediately while retaining the article; over the article it scrolls
only the article (or the connected-page list). Wheel handling reads cached frame
hits, so it neither reloads the vault nor renders a frame to discover the pane.
Collapse all / Expand all are clickable, hoverable controls; Ctrl+Left /
Ctrl+Right provide keyboard access. They clear search, preserve the current
article, and fold nested folders consistently. Folding remains an explicit
whole-tree operation. These interactions require no model requests.
