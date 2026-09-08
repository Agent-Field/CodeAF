# 03 · Dedup the interrupt fan-out
Pareto: WALL TIME + COST down, quality neutral. Evidence: F13/F17.

ONE Esc fired ~6 calls: two mastermind replans, two IDENTICAL title calls, a 57-message
handoff re-read, a reflex call. The 36.7s glm-5.3 replan is the visible stall.

Fix: serialize interrupt handlers behind ONE "what changed" decision; dedup the title
and replan calls; do not re-read the whole conversation for a redirect.

Test: one Esc produces at most one planner pass and one title call.
