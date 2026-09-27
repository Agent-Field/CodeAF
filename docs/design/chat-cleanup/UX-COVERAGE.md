# Conversation UX acceptance

The shared display policy separates who a message addresses from whether that
message finished. Human questions, steering, completed answers and explicit
interim updates remain conversation. Tool activity, reasoning, internal team
traffic, source metadata and maintenance records remain available under
disclosure. Closed active work uses at most three rows of the existing scrolling
animation. Approval and pending-decision controls remain actionable.

| Boundary | Required result | Regression coverage |
| --- | --- | --- |
| Interim update followed by tools | Human update stays formatted and visible once; protocol marker stays hidden even across fragment splits | `feed_update_test.go`, `chat_steering_clean_test.go` |
| Steering during tool arguments | Unexecuted tool payload disappears; steering remains; new response gets a fresh audience boundary | `feed_update_test.go`, issue #1609 |
| Stop during human update | Partial update stays readable and explicitly interrupted; completion is not asserted | interrupted-update transition tests, issue #1620 |
| Retry | Undelivered attempt is discarded; earlier durable updates survive | `feed_update_test.go`, transition tests |
| Resume, compaction, rewind | Source-owned audience and interruption metadata survive; identical words in different message occurrences do not share classification | `presentation_test.go`, issue #1617 |
| Failed tool | Compact failure indicator; full error available on request | `chatcleanup_test.go`, live failure-recovery scenario |
| Task completion and dismissal | Compact receipt; dismissal removes it without deleting work; undo restores it; pending decisions cannot be dismissed | `taskdismiss_test.go`, live task scenario |
| Task result provenance | Repeated original briefs stay folded; source links and full requests remain inspectable | `taskreplytag_test.go`, issue #1613 |
| Internal manager activity | Team traffic and injected wake guidance fold; actual answer and explicit UI responses remain visible | `chatcleanup_test.go`, `async_command_visibility_test.go`, manager replay + live follow-up |
| View and width changes | Ordinary conversation, task and nested transcripts share the policy; compact activity remains within three rows | `chatcleanup_test.go`, `chat_steering_clean_test.go`, `livesteps_test.go`, transition tests |
| Disclosure and later completion | Opening preserves all details; closing restores compact view; completed work collapses again | `nested_disclosure_test.go`, `livesteps_test.go`, real terminal captures |

## Compatibility boundary

Older journals did not record audience or interrupted-response metadata. Complete
reserved interruption records can be folded safely. Ambiguous older mixed prose
remains intact, because deleting a matching phrase could remove an actual human
answer. Newly written records carry typed metadata and do not need that guess.

## Evidence discipline

Recordings are sampled from the real tmux framebuffer with wall-clock timing.
Contiguous GIF excerpts run at 1x speed; the full original cast is preserved.
Historical manager traffic is explicitly labeled as a fixture with a live
follow-up. Practical workflows use a real generated reimbursement program and
independent invocations of that program, not just screenshots of claimed output.

All live requests, including auxiliary model roles, are audited against
`deepseek/deepseek-v4.1-flash` on OpenRouter. Model audit files omit credentials and
prompt bodies. Failed intermediate runs remain identified as failures; only the
final frozen revision's checks support readiness.
