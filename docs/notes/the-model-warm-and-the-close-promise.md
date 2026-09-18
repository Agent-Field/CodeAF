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

## The fix

`warmV3Models` now takes a `context.Context` and waits on `Catalog.Warmed(ctx)`
(`internal/catalog/catalog.go:501`) before it reads or writes anything
(`cmd/codeaf/chatv3.go:2317`); a close that cancels ends the wait there and the
function returns without writing. Both doors seat it on the tracker instead of
`guard.Go` — `proc.warmModels("chatv3/models", …)` at `cmd/codeaf/chatv3.go:419`
and `proc.warmModels("engine/models", …)` at `cmd/codeaf/engine.go:708` — through
`v3Process.warmModels` (`cmd/codeaf/chatv3_process.go:381`) into
`poolErrandGoCtx` (`cmd/codeaf/poolindex.go:164`), whose context
`stopPoolErrands` cancels and whose `WaitGroup` `closeAll` waits on at
`cmd/codeaf/chatv3_process.go:416`.

## One claim about the fix that was wrong

The summary this change was reported with said `warmV3Models` "no longer resolves
the catalog via `ContextLength`". **It does**: `cmd/codeaf/chatv3.go:2320` still
asks `models.ContextLength(started)` for the window. The sentence is false and the
code is right, and it is worth writing down why, because the obvious way to make
the sentence true would be a regression.

The obvious way is to read the window through the non-blocking seam the launch
already uses — `v3ContextWindow(v3Models(models), started)`, which is what
`v3Window` does (`cmd/codeaf/chatv3.go:2279`). Three reasons not to:

1. **It matches the id differently, and worse.** `Catalog.ContextLength`
   (`catalog.go:579`) looks the id up in the rows' own index through
   `normalizeID` (`catalog.go:1291`), which strips a reasoning-effort suffix
   (`roles.SplitEffort`) and a leading `~`. `v3ContextWindow` (`chatv3.go:2283`)
   compares raw row ids with `strings.EqualFold`, so a conversation started on
   `model:high` answers 0 and the session keeps its conservative window — the
   exact "a 1M-token model stops compacting at 128k" failure this function's own
   comment says it exists to correct. `v3Window`'s comment already states the
   division of labour: the launch seam guesses cheaply, and
   "[warmV3Models] corrects it in place once the catalog resolves".
2. **It would not make the warm non-blocking anyway.** The very next line reads
   `models.FetchedAt()` (`chatv3.go:2323` → `catalog.go:430`), which goes through
   the same waiting door `ContextLength` does. Making the warm genuinely
   non-blocking means changing `internal/catalog`'s `FetchedAt` and
   `ContextLength` contracts — a widening this task forbids, for no behaviour
   anybody asked for.
3. **Neither read can wait, and that is the whole point of the ordering.**
   `LoadLazy` closes `resolved.warmed` (`catalog.go:376`) INSIDE its
   `sync.OnceValue` body, so an observed close means the future has resolved;
   `Catalog.rows` (`catalog.go:439`) then answers at once. After
   `Warmed(ctx)` says yes, the blocking door is a map lookup. The
   context-observing wait is the ONLY wait in the function, and it is the one the
   close can end.

So the accurate sentence is: the warm's *wait* moved from the blocking door to
`Catalog.Warmed(ctx)`; its *reads* still go through the blocking door, and are
safe because the wait already landed the rows. A cancel that arrives AFTER
`Warmed` answered true does not stop the write — the join does, by holding
`closeAll` until the write has returned, which is the promise the close makes.

## The failing test, on this tree

`cmd/codeaf/chatv3_modelswarm_test.go` holds the catalog fetch open on a test
endpoint, moves `CODEAF_HOME` to a directory the test owns, closes the process,
releases the fetch, and then watches the moved root for `v3/models.json`.

With the fix in place it passes:

```
$ go test ./cmd/codeaf -run 'TestTheLaunchesModelWarmStopsWhenTheProcessCloses' -count=1 -v
=== RUN   TestTheLaunchesModelWarmStopsWhenTheProcessCloses
--- PASS: TestTheLaunchesModelWarmStopsWhenTheProcessCloses (2.12s)
PASS
ok  	github.com/Agent-Field/codeaf/cmd/codeaf	2.153s
```

The 2.12s is the test watching the moved root for its whole two-second window
before it will agree nothing landed there; it is not a slow path.

Against the pre-fix shape — `v3Process.warmModels` reduced to the fire-and-forget
`guard.Go(scope, func() { warmV3Models(context.Background(), …) })` it replaced —
it fails, deterministically and in a tenth of a second, with the write landing in
the directory the test had already moved on to:

```
$ go test ./cmd/codeaf -run 'TestTheLaunchesModelWarmStopsWhenTheProcessCloses' -count=1 -v
=== RUN   TestTheLaunchesModelWarmStopsWhenTheProcessCloses
    chatv3_modelswarm_test.go:67: the model warm wrote /tmp/TestTheLaunchesModelWarmStopsWhenTheProcessCloses2865349609/006/v3/models.json after the process closed.
        The warm resolves CODEAF_HOME at the moment it writes, so a warm started outside the
        profile's errand tracker lands in whichever root is current when the rows arrive — the
        TempDir clean-up's own "directory not empty". It must be seated on the tracker and joined
        at close (chatv3_process.go, poolindex.go).
--- FAIL: TestTheLaunchesModelWarmStopsWhenTheProcessCloses (0.11s)
FAIL
FAIL	github.com/Agent-Field/codeaf/cmd/codeaf	0.139s
```

That is the production `unlinkat …: directory not empty` reproduced on demand,
with no load and no timing to hope for: the only difference between the two runs
is whether the warm is seated on the tracker.

## The reading could not settle

The engine daemon's process (`cmd/codeaf/engine.go`'s `engineProcess`) is a
`sync.Once` singleton with no close in the tree, so its own pool errands are
never joined either. This did not need settling for the fix, because the errand
tracker is a package-level map keyed by PROFILE DIRECTORY
(`poolErrandSet`), not by process: `chatv3_host_test.go` opens a second process
on the same profile and its `t.Cleanup(proc.closeAll)` cancels and waits the same
tracker the singleton's warm was seated on. A production `codeaf engine` daemon
ends with its process, where the join does not matter.
