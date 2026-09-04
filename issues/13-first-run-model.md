# 13 · Default first-run model must answer
Pareto: WALL TIME + UX. Evidence: F42 — blind user's default deepseek-v4-flash hung 90s
on first prompt; had to /model to grok-4.6.

Fix: the shipped default chat model must be chosen for first-run reliability, and a
first-prompt stall must surface a clear rescue/switch path, not a silent hang.

Test: a fresh profile's first prompt gets a streamed answer within a bounded time.
