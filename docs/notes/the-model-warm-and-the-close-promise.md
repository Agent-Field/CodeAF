# The model warm, and what the process's close promises

## The writer

`warmV3Models` (`cmd/codeaf/chatv3.go`), started once per boot conversation by
the two v3 doors:

- `cmd/codeaf/chatv3.go` — the local door (`codeaf chat` / `codeaf resume`);
- `cmd/codeaf/engine.go` — the `codeaf engine` daemon, once per hello.

It first resolves the model catalog — a network round-trip on a cold cache — and
then refreshes the picker's cache through `tui3.WriteModelCache` →
`tui3.WriteModelCacheFor` → `tui3.ModelCachePathFor` → `home.Join("v3",
"models.json")`. **That path is resolved at the moment it writes, not when the
warm started.** It was started with `guard.Go`, and `guard.Go`
(`internal/guard/guard.go`) joins nothing at shutdown.

## Why it lands in the next test's directory

`t.Setenv` restores `CODEAF_HOME` when a test ends, so a warm that outlives its
own test and reads the variable at write time lands in whichever `TempDir` the
*current* test has just set. `t.TempDir`'s clean-up then sees a file appear under
a directory it is removing and fails with `unlinkat …: directory not empty`.
Only under load, because the goroutine must be slow enough to cross a test
boundary; never alone, because there is no earlier test to leak one.

The two named failures are both this one writer:

- `TestTheVisionGateReadsTheDiskCacheWhileTheCatalogIsWarming`
  (`cmd/codeaf/chatv3_images_test.go`) follows
  `TestTheEngineDoorKeepsTheAmbientSideOnOverAConnection`
  (`cmd/codeaf/chatv3_host_test.go`), which boots the engine and starts the
  `engine/models` warm.
- `TestOpeningAndClosingConversationsLeavesTheProcessHoldingNothing`
  (`cmd/codeaf/chatv3_track_test.go`) is a later victim of the same warm.

Traced with a temporary instrument on `internal/guard` and the three
write-time resolvers on a full `go test ./cmd/codeaf` run: the only
`WriteModelCache` calls outside a test's own body were from the
`engine/models` scope.

## The owner, and what the close promised

The owner is the process, `v3Process`, built once per door by
`openV3ProcessWith` (`cmd/codeaf/chatv3_process.go`). Its close door is
`v3Process.closeAll`, and its own comment claims, verbatim:

> The start-up errands this process seated on its profile — the pool index
> refresh and the outbox push — were started fire-and-forget. `stopPoolErrands`
> cancels them and waits, so **nothing this process started is still writing
> under its profile once it closes**.

That is the gap: the model warm is a start-up writer this process started, and
`closeAll` did **not** wait for it — it was never seated on the tracker
(`poolindex.go`'s `poolErrands`/`poolErrandGo`/`stopPoolErrands`, the machinery
#1124 built for exactly this failure and its `poolleak_test.go` race). The
`guard.Go` call site is the evidence that hypothesis 2 holds: the close returns
while a writer it should own is still running, because the writer was never
registered with it.

## The reading could not settle

The engine daemon's process (`cmd/codeaf/engine.go`'s `engineProcess`) is a
`sync.Once` singleton with no close in the tree, so its own pool errands are
never joined either. This did not need settling for the fix, because the errand
tracker is a package-level map keyed by PROFILE DIRECTORY
(`poolErrandSet`), not by process: `chatv3_host_test.go` opens a second process
on the same profile and its `t.Cleanup(proc.closeAll)` cancels and waits the same
tracker the singleton's warm was seated on. A production `codeaf engine` daemon
ends with its process, where the join does not matter.
