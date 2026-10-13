// Package runs is the runs engine for runs/*.yaml: steps, loops, gates, waits,
// retries, budgets, triggers, approvals and connector fakes. Lane B4.
//
// Run state is a SQLite journal under <root>/<app>/runs so a restarted sidecar
// continues a run that was waiting or still marked running. Wave 1 connector
// calls are fakes: real connectors are not in this wave, and an outbound verb
// (send, post, pay) never runs until a person approves it.
//
// Service.Close cancels and joins trigger workers and in-flight runs, then closes
// the shared journal. Register it after allocating a temporary root so shutdown
// finishes before directory cleanup; server owners must also call it on shutdown.
package runs
