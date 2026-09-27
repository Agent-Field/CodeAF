# PR 1622 protected live acceptance

Goal: use an existing pantry CLI to create a useful shopping report, approve a weekly refresh from a new profile while a different profile owns the shared timer, then choose manual use and stop the weekly item.

Exact live runtime: dddee8c4e (embedded version checked before launch), binary SHA256 55ad4795cbb8be78fb4478adf3729a1be3e45c1f2d196d5638808003a46cf55f. Final checkout 7eafed309 differs only in two test fixture corrections. Spark, fleet-sh tmux session critical-extended-1618verified-v3. All 22 recorded model receipts use deepseek/deepseek-v4.1-flash through OpenRouter, including low/reflex/talk/worker. --one-model; model_pool off is the disclosed workaround for separately fixed #1608/#1610.

Outcomes: CLI actually ran and SHOPPING.md contains beans buy3, rice buy1; inventory stayed byte-identical. Approval saved the weekly item but preserved BOTH preexisting service and timer bytes. The model explicitly explained the schedule needs a codeaf window open for this home and cannot run after it closes. A natural follow-up stopped the weekly item through the product, retained the useful report, ran19existing tests and printed the real shopping output. Independently rerun tests passed and stored item status is retired; no future schedule remains in this fixture.

Evidence: honest_receipt snapshot shows owner refusal and model limitation; final_checks snapshot shows19tests, practical command output, stopped item. Original recording is closed and unmodified; GIF may compress waits but must be labeled. See execution.json, usage.json, proof.json and artifact-tests.log.

Limits: real ownership-preserving Linux refusal is live; Darwin, concurrent claims, canceled locks and explicit takeover are fake-home deterministic regressions, not live changes to the shared OS service. This task never changed the real shared timer. Background execution after the window closes was intentionally unavailable, not tested as successful. The inherited UI card still uses pre-#1612 generic check wording; separate PR1612 owns task wording.

Earlier1618.cast revealed missing model-facing availability; fixed before this runtime. A second1618final.cast accidentally launched the old binary during rebuild; hash inspection caught it, its execution manifest was corrected, and it is excluded from final-runtime acceptance. Both old fixtures were stopped and retained locally.
