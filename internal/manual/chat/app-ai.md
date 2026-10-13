# AI calls from the codeaf app

## Can the app call AI, choose a model, make titles or summaries?

The app calls AI only through the binary's authenticated
`POST /app/v1/ai/call`. The binary owns model routing, the day cap and the
spending ledger. The app keeps a feature's prompt text and never names a model
on this route. Agent names remain deterministic.

No app AI feature is enabled in this build. `app.title`, `app.summary`, unknown
purposes and terminal roles return `this app AI role is not registered` with
HTTP 422. An owner-approved feature must be registered in the binary first.
Having this route does not enable Inbox summaries, AI ranking or AI titles.

## AI call budgets, costs, receipts and limits

A registered call needs positive two-decimal USD budget, 1–128 nonempty text
messages and `max_tokens` from 1 to 32768. The request must fit in 32 KiB.
Only system, user and assistant messages are accepted. An optional tier is
`reflex`, `low` or `worker`; a saved role pin still wins. `expect` is refused.
JSON schemas use local references only; the result must match the schema.

The binary checks the call budget and remaining day cap before each provider
send. Too little budget returns HTTP 402 `at_cap`; an unknown price or missing
output ceiling returns HTTP 503. Estimates only control admission. The ledger
records actual provider costs under `who:{kind:"app",purpose}` and retains
sub-cent receipts. AI replies keep eight decimal places and a `ledger_id`.
An answer with no recoverable usage receipt has unknown cost and returns an
error. Token counts without a dollar cost also need a receipt; direct services
cannot recover one from the router. A reported zero cost is accepted as zero.
A ledger failure also refuses or reports that its receipt could not be
saved. An already admitted provider call finishes before another can start.

## Streaming an app AI reply, retrying and cancellation

`stream:true` returns SSE `ai.delta` while text arrives and `ai.done` with the
final model, usage, cost and ledger id. These are on the call response, separate
from the main app event stream. A failure after streaming starts emits `error`
with its message and ledger id, without `ai.done`. Disconnecting or canceling
the launch stops provider work; billed work still keeps its receipt.

An Idempotency-Key replays the same response without another model call within
this launch's 128-entry cache. Changed input with that key returns HTTP 409.
Keyed mutations wait for the original response to finish. App calls sharing a
home run one at a time; a concurrent call returns HTTP 409.
