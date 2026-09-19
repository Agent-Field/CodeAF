# Survivor leaf-end findings

Run folder: `audit-notes/c303-leaf-end/`.

## Task

Map the ordering at the `internal/exec` leaf-end seam that terminates survivor processes and records how many were ended. This task changes only this evidence note.

## Settled facts

- `internal/exec/jobs_test.go:410` names `TestLeafEndTerminatesSurvivorsAndNotesCount`.
- A gate run under box load once reported `process 0 survived leaf end`.
- The named test passes when run alone.
- Leaf end is intended to terminate survivor processes left by a leaf and note the number ended.
- The reported failure is confined to `internal/exec`.

## Not yet established

The deciding event ordering, whether the defect is test-side or product-side, real-leaf reachability, and the smallest deterministic correction remain open until the named seam is inspected.

## Ordering map

1. `internal/exec/jobs_test.go:372-375` asks for `echo $$ > survivor.pid; sleep 30` as a background command.
2. `internal/exec/jobs.go:155-160` holds the registry mutex and rejects starts after the leaf is closed. Thus start versus close has a single order.
3. `internal/exec/jobs.go:186-209` starts the process, captures its process-group identity, and inserts the running job while still holding that mutex.
4. `internal/exec/jobs.go:217` starts the sole waiter after registration.
5. `internal/exec/jobs_test.go:377-389` waits until `survivor.pid` exists before returning the final answer. This proves the command started before leaf teardown.
6. `internal/exec/linear.go:773` calls `tools.Close()` during leaf teardown before `Run` returns.
7. `internal/exec/jobs.go:792-806` claims the one-shot sweep under the same registry mutex, marks the registry closed, selects every running unkept and unpromoted job, and marks each stop requested. A concurrent start is therefore either fully registered and selected or refused.
8. `internal/exec/jobs.go:827-845` sends TERM to all selected groups, waits on each `job.done`, escalates all unfinished jobs to KILL after the grace period, then still waits for every `job.done`.
9. `internal/exec/shellwait_linux.go:17-24` waits for shell exit without reaping, records that wait, terminates detached descendants while the leader identity remains authoritative, and only then calls `cmd.Wait` to reap the leader.
10. `internal/exec/jobs.go:266-287` publishes `job.done` only after `reapShell` and `cmd.Wait` have completed and the terminal state is stored.
11. `internal/exec/jobs.go:785-787` publishes the selected count and returns it only after `reap` has waited for every selected job.
12. `internal/exec/linear.go:774-785` adds the returned count to the leaf result and refreshes artifacts before teardown returns.
13. `internal/exec/jobs_test.go:396-410` receives the completed leaf result, checks the count, reads the recorded numeric PID, then uses `kill(pid, 0)` as its liveness assertion.

## Decision

The observed one-off failure is a test-side observation hole, not evidence of a product survivor leak or count race. Product close cannot return while a selected job's waiter remains unsettled, and the waiter cannot settle before `cmd.Wait` reaps the recorded shell leader. Registration and close are mutually exclusive under one mutex, so the count cannot omit a command that successfully registered before close. The test, however, discards process identity and probes only the integer PID at `internal/exec/jobs_test.go:408-410`. After `cmd.Wait` reaps the original process and before that probe, Linux may reuse the PID for an unrelated process. Under that ordering `kill(pid, 0)` succeeds and falsely reports that the original survivor lived.

Real leaf-end reachability of the alleged leak is ruled out by the wait chain above. The PID-reuse ordering is reachable only in the test assertion because product signaling uses the captured group identity and checks ownership, while the assertion uses a bare PID.

The smallest deterministic correction belongs in the test: retain and compare process identity, or force PID reuse behind a test seam and assert that a reused PID is not treated as the original process. Merely polling `kill(pid, 0)` is not a valid settled-state wait because it can continue to observe an unrelated reused PID. No product change is supported by this evidence.

## Ruled out

- Late registration after sweep: start and claim-sweep share the registry mutex, and starts see `closed`.
- Count published before termination settles: `publishSweep` follows `reap`, which waits for every `job.done`.
- `job.done` published before the shell is reaped: Linux `reapShell` calls `cmd.Wait` before the waiter closes `done`.
- Detached-child cleanup after leader identity disappears: Linux performs the detached sweep while the leader remains an unreaped zombie.
- Test reading before leaf teardown: `linear.Run` returns only after the teardown defer completes.
- A bare count-read race: both first and later close callers wait for publication, and `closedCount` is mutex protected.

## Forced-ordering reproduction plan

Before changing the test, the next step will add the smallest test-only liveness-probe seam beside the existing bare-PID assertion. The test will force the post-reap PID-reuse observation by substituting a successful probe only after `Linear.Run` has returned. This preserves the production close ordering, creates no load or busy loop, and deterministically demonstrates that the current assertion reports a survivor solely from an integer PID that can now denote another process. The focused test is expected to fail with the existing `survived leaf end` message; that failing test-only step will be committed intentionally for the correction task.

## Reproduction edit correction

The first test edit committed the test-only probe hook but the exact replacement of its call site was refused because two bare `syscall.Kill(pid, 0)` assertions exist in this file. No focused test ran, so commit `70c49222b` is only the hook half of the reproduction. The next edit will target the named leaf-end assertion with surrounding failure text, then gofmt, run the focused test, and commit the deterministic failing state.
