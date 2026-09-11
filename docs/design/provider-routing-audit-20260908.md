# Conversation routing and OpenRouter client audit

The objective is less time to useful text and completed tool operations, with
bounded extra spend. Provider identities, tariffs and performance come from the
catalog and receipts. No provider preference list or price table is added.

## Scope and findings

Inspected the conversation caller (`session/agent.go`, `loop.go`, auxiliary
callers), router/client construction, request encoding, reasoning and cache
fields, endpoint/refusal repair, retry and admission paths, HTTP transport,
SSE decoding, progress and timing measurements, recovery execution, the lane
chooser and ledger, sheet refresh, and receipt reconciliation. This audit concerns
chat and task completions; media generation is not an interchangeable routing arm.

| Finding | Change and reason |
| --- | --- |
| Fixed 400-readable/2000-hidden token assumptions undervalued tool completion speed. | Learn visible and hidden output by model, tool availability, intent and resolved reasoning setting. Use conversation history when evidence is absent or stale; bound estimates by the actual wire ceiling. |
| Unknown output was effectively free output in candidate pruning. | Keep capable alternatives when answer size is unknown; do not derive a stricter output-price ceiling without its denominator. The transport price ceiling still applies. |
| A successful cold-cache response released the preferred endpoint. | Keep a successful endpoint through missing cache reports, expiry and changed prefixes. Failures and excessive charges still release it. |
| Cache affinity changed the wire's first preference after the wait monitor chose its clock. | Build the generated monitored choice from the final wire preference. |
| A whole phrase counted as one visible token. | Estimate progress from cumulative bytes using the existing token approximation. Fragmentation and batching now yield the same count; bills still use receipts. |
| Tool-only replies recorded their whole duration as first-token waiting. | Start the first-token clock on tool fragments too, preserving the generation interval. |
| HTTP faults could repeat the same request before funded recovery ran. | Hand faults to the existing recovery controller when it can afford an alternative. Account rate limits retain backoff; exhausted recovery retains bounded retries. |
| Retryable HTTP error bodies had no streaming idle bound. | Apply the existing body watchdog before either error or success bodies are consumed. |
| Cache preference could displace or remove a borrowable explicit pin. | Explicit preference takes precedence in both the wire order and monitored choice. |
| A failed strict primary could walk to another endpoint. | Automatic failure recovery cannot leave a strict primary; an explicitly accepted rescue remains possible. |
| Recovery used the client's default model even when the request overrode it. | Use the effective request model for recovery candidates, beliefs and cost estimates. |
| The legacy affinity header was not the documented OpenRouter session header. | Also send `x-session-id`, hashing overlong identities stably to obey its protocol limit. |

The session-header behavior and its interaction with explicit provider ordering
were checked against [OpenRouter's cache documentation](https://openrouter.ai/docs/guides/best-practices/prompt-caching).
Its streaming documentation distinguishes keepalive comments from generated
output; the controller already observes that distinction and retains it.
[Streaming reference](https://openrouter.ai/docs/api_reference/streaming).

## Paths retained and limits

- Endpoint refresh runs in the existing background beat. Choosing reads local
  evidence; no synchronous endpoint or price fetch is added.
- HTTP connection pooling remains shared. The SSE decoder consumes arriving
  frames without waiting for a large buffer. The benchmark guard likewise uses
  `read1` and flushes forwarded chunks.
- Session and leaf cache identities remain stable through retries. UI event
  subscribers enqueue events rather than blocking the provider on drawing.
- Reasoning budgets, parameter repair and refusal handling remain independent
  of provider rankings. A strict user constraint is not silently relaxed for speed.
- Interrupted generation accounting remains asynchronous. Cancelling a local
  request does not guarantee the upstream stops billing it.
- The controller still learns waiting distributions and compares the remaining
  wait with available alternatives under its existing recovery budget. TCP
  packets and proxy comments provide no reliable provider queue position.
- Account throttling, simultaneous provider degradation, and model reasoning
  can still cause long waits. Existing rate-limit and outer session retry budgets
  remain; this change does not establish a global end-to-end latency guarantee.
- Answer-size averages are deliberately small and auditable. They do not yet
  condition on prompt length or predict the difficulty of a new GitHub issue.

## Validation

Regression tests exercise the real request adapters and controller seam for
batched text, tool-only timing, provider failure and request-model override,
strict and borrowable pins, stable cold-cache identity, both response transports,
and throughput-sensitive tool routing. Ledger tests cover cross-process replay,
compaction, stale evidence, out-of-order observations and bounded retention.

The live comparison must use the same pinned issue baseline, model, isolated
container, network guard and fresh session state for control and candidate.
Measure completed tests and task time alongside first response, provider changes,
cache usage, retries and cost. Unit tests demonstrate corrected mechanisms;
they do not establish the size of a real-provider speedup.
