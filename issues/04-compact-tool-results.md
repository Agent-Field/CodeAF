# 04 · Compact old tool results mid-turn
Pareto: COST + WALL TIME down, quality held. Evidence: F9, turn compl/prompt = 0.047.

The turn loop re-reads the full conversation every tool round-trip (~21x more read than
written). Old tool results are the bulk. The digest budget already proved a 5k-token
account replaces a 57-91k raw read for the checkpoint reader — apply the same to the
turn loop's history.

Fix: compact/stub old tool results once their evidence is consumed, keeping the newest
result verbatim. Not the system prompt (already cache-friendly).

Test: a long multi-tool turn's prompt growth is sub-linear in tool calls; quality
(measured by the audit verdict) is unchanged.
