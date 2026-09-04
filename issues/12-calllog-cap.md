# 12 · Raise the debug call-log cap when bodies are on
Pareto: DX. Evidence: F24 — AFORGE_CALL_LOG_BODIES=1 makes each record ~40KB; the 32MB
cap rotates after ~80 calls, too small for a real debug session.

Fix: when bodies are on, raise or disable the cap (or keep N generations, not one).

Test: a >80-call debug session with bodies retains full history.
