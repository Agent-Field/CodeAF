# 02 · Fire the hedge on a stall
Pareto: WALL TIME down, quality neutral. Evidence: F33.

A first turn waited 129s on ONE lane with `arms: None` — the hedge never fired, so the
user sat through "all lanes slow · still waiting". The hedge machinery exists (it fired
elsewhere) but not on this stall.

Fix: arm the hedge/rescue when a lane exceeds a latency bound even if no error has
arrived; do not wait for a terminal error.

Test: a stalled lane triggers a second arm within the bound; TTFT is bounded.
