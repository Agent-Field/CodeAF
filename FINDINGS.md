# c294 findings

The hosted chat surface receives a `countingAgent` embedding `*remote.Agent`, but the remote client currently exposes only `PlanSpend` from the run API. Consequently the surface cannot satisfy its run-plan agent interface in hosted conversations. This task will carry the complete task reads, steering verbs, and run-summary operations across the engine-host wire, preserve store refusal sentences, honor refresh cancellation, and prove the remote client satisfies the shared interface and reads a seeded real store end to end.

The implementation must keep one method-set source of truth, omit unusable capabilities, add failing tests before implementation, and commit each independently passing step.
