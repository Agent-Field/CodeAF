// Package runner is a bug-for-bug port of src/effect/runner.ts (the 4-state
// per-session runner), src/session/run-state.ts (the SessionID→Runner registry
// and Session.BusyError) and the `Effect.timeout` + `Effect.catchCause`
// collapse idiom that ENGINE-DESIGN.md §4.5 names `degrade` — plus the
// Effect `Cause` trichotomy (§4.3) all three are built on.
//
// ── seams ─────────────────────────────────────────────────────────────────
//
//   - Fibers → goroutines. `Effect.forkIn(scope)` (used for runs) becomes a
//     goroutine on a context derived from the Runner's scope context;
//     `Effect.forkChild` (used for shells) becomes a goroutine on a context
//     derived from the CALLER's context, so a caller that goes away takes its
//     shell with it and does not take a run with it.
//   - Interruption → context.WithCancelCause(parent) with the cause
//     ErrInterrupted. Work bodies observe it through ctx.Done() and should
//     return context.Cause(ctx); returning a bare ctx.Err() is normalised at
//     the runner boundary (normalizeCancellation) so it still classifies as an
//     interrupt instead of an ordinary failure.
//   - Deferred → deferred[A] (a one-shot channel plus a settled value).
//     SynchronizedRef → sync.Mutex. Latch → Latch. `let ids = 0` → a float64
//     field, because every JS number is a float64 in this port.
//   - Panics → Defect reasons. Every goroutine the runner starts, and every
//     Value call, wraps its body in recover().
//   - There is no injectable clock or RNG: neither TS module reads one. The
//     only time-dependent surface is Value's deadline, which takes a
//     time.Duration from the caller.
//
// ── sibling ports ─────────────────────────────────────────────────────────
//
// Deliberately none. This package imports only the standard library plus
// internal/jscompat (JS number formatting inside Pretty) and internal/logshim
// (the discarding log sink Log writes to). It is generic in the result
// type A rather than typed to a message struct, exactly as runner.ts is
// generic in <A, E> and run-state.ts instantiates it at MessageV2.WithParts.
//
// ── fidelity notes (deliberate, do not "fix") ─────────────────────────────
//
//   - ENGINE-DESIGN §1 splits this material across engine/fault,
//     engine/degrade, engine/runner and engine/runstate. It ships as one
//     package (one file per concern: fault.go, degrade.go, runner.go,
//     registry.go) because the four are a single assignment and Go would
//     otherwise force fault→runner→runstate import plumbing for ~15 exported
//     symbols. Splitting later is mechanical.
//
//   - §4.3 gives `IsInterruptOnly(err)` as
//     `errors.Is(err, ErrInterrupted) && !HasDefects(err)`. That is NOT what
//     Effect does: `Cause.hasInterruptsOnly` is
//     `reasons.length > 0 && reasons.every(isInterrupt)`
//     (effect/dist/internal/effect.js:114), so a Fail+Interrupt cause is
//     interrupt-only under the design's formula and is not under Effect's.
//     runner.ts:162-164 exists precisely to tell those two apart, so this port
//     implements the Effect semantics: an explicit multi-reason Cause with
//     all-interrupts / any-interrupt / any-die predicates.
//
//   - Value swallows interruption too, including cancellation inherited from
//     its outer context. ENGINE-DESIGN.md:1174-1182 explicitly makes
//     catchCause a complete collapse and calls out this shutdown-hostile edge
//     as a bug candidate. Do not special-case context.Canceled.
//
//   - Value waits for fn to return after its deadline trips instead of
//     abandoning it. `Effect.timeout` interrupts the inner effect and awaits
//     its finalizers (probed: a 60 ms onInterrupt finalizer under a 20 ms
//     timeout makes the whole call take ~91 ms), so a Go fn that ignores ctx
//     hangs the call exactly like an uninterruptible Effect would.
//
//   - Runner.StartShell PANICS when the runner is not Idle: `opts.busy()`
//     (which run-state.ts:63-65 defines as `throw new BusyError`) is called
//     from inside Effect.sync at runner.ts:146-149, i.e. it becomes a fiber
//     defect, and the `throw new Error("Runner is busy")` on the next line is
//     unreachable whenever the hook is installed. Both are kept, both panic.
//     Registry.AssertNotBusy, whose TS also `throw`s, returns *BusyError
//     instead — it is a pure predicate with no fiber to defect into, and every
//     caller of it is a Go caller.
//
//   - `onIdle` runs OUTSIDE the runner mutex and `onBusy` runs INSIDE it. That
//     asymmetry is TS's: every site returns `idle` as the *effect* half of
//     SynchronizedRef.modify (so it runs after the ref is released), while
//     startShell yields `busy` inside the modifyEffect body (runner.ts:153).
//     The window it opens — a new run can start between the state going Idle
//     and onIdle firing — is real in both languages.
//
//   - `ensureRunning` never calls `onBusy`. Only startShell does. So on the
//     codeaf path a plain turn never sets session status "busy", but does set
//     "idle" when it finishes. [BUG-CANDIDATE] — preserved.
//
//   - When the runner is already Running or ShellThenRun, `ensureRunning`
//     DISCARDS the caller's work and hands back the in-flight run's result
//     (runner.ts:120-122). Two different prompts therefore return the same
//     assistant message. Preserved.
//
//   - `stopShell` awaits the ready latch with no deadline and no context
//     (runner.ts:110 swallows even the await's own exit). A shell that never
//     opens its latch wedges Cancel forever, in TS and here.
//
//   - The registry DELETES the runner on idle (run-state.ts:57-60), so `ids`
//     restarts at 0 for the next turn of the same session. Id monotonicity is
//     per-runner, never per-session.
//
//   - Pretty() reproduces Effect's causePretty for the stack-free case: Go
//     errors have no `.stack`, which is exactly the branch that renders
//     `${name}: ${message}`. An interrupt-only cause renders the nested
//     InterruptError/InterruptCause block, fiber ids included, verbatim.
package runner
