---
kind: fixed
title: a model that keeps refusing is given up on, and the reply finishes on the next one in the chain
pr: 794
surface: [chat, engine]
invalidates:
  - "The fallback chain was reachable from ONE road only. `internal/session`'s `completeWithRetryReasoning` hopped to `nextFallback` from the stream-cut branch (`provider.CutFrom(err)`) and nowhere else; a refusal walked `Limits.TransportAttempts` with `backoffWait` and then returned `after %d retries: %w`. The 2026-08-24 failover wave's note that the chain was reached from '429-patience exhaustion' was a claim about internal/provider's own in-call patience, never about the turn loop — measured false on 2026-09-10, conversation 57d51779f63ac603: one 502 and three 429s from 13:00:17 to 13:01:32 ended a turn on deepseek/deepseek-v4.1-flash while z-ai/glm-5.3-flash answered every task-node call in the same session. BOTH roads now end in one verdict, and a spent budget with a model left to ask moves."
  - "`internal/taxonomy`'s transport policy had two endings, ActionRetry and ActionGiveUp. It has three: ActionHop is a spent budget WITH somewhere left to go, chosen on `Evidence.FallbackAvailable`, which is the caller's fact — the policy never names a model. `Verdict.Hops()` is the reader. ActionHop is a Transport verdict, so it still ends no turn and buys no tier."
  - "The cut allowances lived in internal/session as `silentRetries`/`babbleRetries`/`blindRetries` (2/1/1 RETRIES) and were spent by `cutBudget`. They are `taxonomy.SilentCutAttempts`/`DegenerateCutAttempts`/`BlindCutAttempts` (3/2/2 TOTAL ATTEMPTS — the same numbers, stated as attempts rather than retries), read by the one policy that decides what any failed request is worth. `cutBudget` and the three constants are deleted; a caller that held its own copy of one is now holding a copy of nothing."
  - "`Evidence` gained `Cuts`, `Degenerate`, `Rerouted` and `FallbackAvailable`; `waitFor` now returns 0 for a cut, as the turn loop always did in practice — an errand that met a cut used to be handed a 2s/4s/8s backoff it did not want."
  - "EventRetrying carried a sentence and nothing else, so a surface drew the same dim line for 'asking the same model again' and for 'that model is done, the rest of this reply comes from another one'. It now carries `Event.Retry *session.RetryNews`: Model, Attempt, Attempts, Reason (the person's words, never the journal's 'the endpoint refused') and Next — the model being moved to, EMPTY on an ordinary retry. It is filled on every EventRetrying the loop sends, cut notices included, and `Event.Text` is still the whole line, so a surface reading only Text is unchanged."
  - "`--one-model` switched the move off by emptying the adapter's chain at the door and nowhere else. `Agent.nextFallback` refuses it directly now, so the promise holds against a completer that offers a chain anyway."
  - "The manual said 'a server fault — a 500, a 503, a torn connection — keeps the short patience it always had and never moves your model'. That is deleted. models-and-cost.md now has 'The model kept refusing and aforge moved to another one', with the three sentences the code spells: `the request failed — asking again`, `the model kept turning the request away — finishing this one on <model>`, and the walked-out-chain ending that names every model tried."
---

The failure was never one bad branch: it was a turn holding TWO answers to "this
model's budget is spent" and picking between them by which kind of failure
happened to arrive. So the answer moved into the object that already answers what
a failed request is worth. `transportPolicy.Decide` walks one budget with two
kinds of spending — an outright failure spends a rung off `Limits.TransportAttempts`
with its doubling wait, a cut stream spends a shorter allowance with no wait,
because the request was served and the *reply* came apart — and the three endings
are the same for both.

`movesForFailure` is deliberately untouched: a task node moving to a dearer model
is a TIER purchase and this is not one. Wiring `FallbackAvailable` into it would
turn one decision into the other, which is the confusion the whole boundary
exists to prevent.
