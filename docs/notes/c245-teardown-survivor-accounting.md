# c245 teardown survivor accounting follow-up

c245 closed the daemon leak, but its teardown report combined pane descendants, workspace helpers, and every started-after-the-run process whose environment carried the measured HOME. On this shared machine that last set included other sessions, so one `teardown_survivors` number made unrelated work look like this measurement's leak.

The fix never sums those sets. `teardown_owned_survivors` contains only pane descendants and workspace helpers for which the run is accountable. `advisory_skipped_home_sweep_matches` separately reports the machine-wide processes a skipped HOME sweep would have matched, and is not attributed to the measurement.

`prior_own_processes` matches the resolved executable, not the run, so a CLI running in another terminal counts and can safely false-alarm rather than risk hiding a real leftover.

## Untimed process-count checks

These checks created no load and measured no duration. The probe executable was a private copy of `/usr/bin/sleep`. `count_exe` used the script's rule, comparing each readable `/proc/PID/exe` resolved path with the probe path.

The clean-state assertion was first checked with no matching process:

```console
$ count_exe "$probe" # clean state before helpers
prior_own_processes=0
```

Two intentionally running copies showed that the same assertion bites. Both PIDs were named:

```console
$ count_exe "$probe" # two intentionally running copies
prior_own_processes=2
prior_own_pids=789 790
confirmed_pid_789_cmdline=/tmp/c252-note-775/codeaf-probe 300
confirmed_pid_790_cmdline=/tmp/c252-note-775/codeaf-probe 300
```

Before cleanup, each `/proc/PID/cmdline` was read as shown above. Only explicit PIDs 789 and 790 were then killed. The second clean-state assertion confirmed cleanup:

```console
$ count_exe "$probe" # clean-state assertion after explicit cleanup
prior_own_processes=0
```

For the busy-box case, one unrelated `sleep` carrying the same HOME was intentionally running. The owned set was empty while the advisory independently reported the current machine-wide count:

```console
$ owned_left=""; print split fields while unrelated HOME process runs
teardown_owned_survivors=0
advisory_skipped_home_sweep_matches=4
confirmed_pid_818_cmdline=sleep 300
```

`/proc/818/cmdline` was read and confirmed before explicit PID 818 was killed. No pattern kill was used.

For the real-leftover case, one probe was intentionally retained in the owned set:

```console
$ owned_left="$left"; print owned field for intentional leftover
teardown_owned_survivors=1
teardown_owned_survivor_pids=825
confirmed_pid_825_cmdline=/tmp/c252-note-775/codeaf-probe 300
```

`/proc/825/cmdline` was read and confirmed before explicit PID 825 was killed. No pattern kill was used.

## Repository checks

`gofmt -l ./internal ./cmd` printed nothing. `go vet ./internal/... ./cmd/...` passed. `go run ./cmd/codeaf-changes check` reported `docs/changes/unreleased: 39 entries, all well formed.`

`go test ./internal/guard ./internal/namelaw -count=1` passed `internal/namelaw` and showed the inherited guard failure at `cmd/codeaf/poolindex.go:132` and `cmd/codeaf/poolindex.go:148`, where `poolErrandsMu.Lock()` is not followed directly by a deferred unlock. This branch predates the trunk fix, so those lock lines were observed and left untouched.

This work is ready. It has not been pushed and no pull request has been opened.
