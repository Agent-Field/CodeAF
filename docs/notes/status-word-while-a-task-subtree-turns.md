# The status word while a task subtree turns

## The word

`working` — `tabWorkingWord` (`internal/tui3/tabsignal.go`). It is the
surface's own vocabulary, already the tab strip's word for the same door
(`tabSignalWord`), and already in the manual's state-word table. Nothing
new was coined and no fourth run state was added.

## Where it appears

`app.stateWord()` (`internal/tui3/render.go:3329`), as a case among the
ones that outrank `a.state` without touching it — copy mode, a sweep's
receipt, a lost room guest, winding down, waiting, a starting task. The
case is:

```go
if a.state == stateIdle && a.frontSignal() == tabWorking {
    return tabWorkingWord, a.pal.accent(tabWorkingWord)
}
```

It is guarded on `a.state == stateIdle` so it fires only for a door that is
genuinely at rest, and it returns the word **without** setting `a.state`:
that field is a behavioural predicate, read in roughly fifteen non-test
places to decide the spinner, the clock, ticking, barge-in, the agent read
and the background-work question (`app.go`, `bargein.go`, `background.go`,
`attach.go`, `detach.go`). A change that made `a.state` say `stateWorking`
while the door is at rest would change all of that and is not what was
asked for.

## What reading could not settle, and the decision taken

**The brief named `runningTasks(agent)` (`hop.go:846`) as the count that
already exists. It cannot be read on a frame.** It goes through
`Agent.TaskIndex()`, which reads a file, and `internal/tui3/tabsignal.go`'s
own header forbids exactly that: the status row is laid out on every frame,
so the same reading here would be a disk scan per frame (or, over `--host`,
a call to another machine). `runningTasks` is drawn on a keystroke — the
switcher, `alt+t`, the close guard — and stays there.

The frame-safe reading of the same question already existed and is where
the fix reads from: **`app.frontSignal()`**, the surface's own answer to
"is this conversation doing anything" (`tabWorking` when a turn is in
flight, a task node is in flight, or a background job is running). It walks
`a.taskSeen` — a map the surface already keeps for the update de-dup — and
`a.jobs`; no lock, no file, no wire. Using it means the status row and the
tab strip cannot disagree about one conversation, which is the whole of the
defect.

**It includes background jobs, not only task nodes.** `frontSignal` is
`a.state == stateWorking || a.tasksInFlight() || a.jobsRunning() > 0`, so a
door at rest with only a background job running now also reads `working`.
The tab strip already drew that door as `working`; leaving the word `idle`
there would re-open the same disagreement one case over. The figures are
untouched either way.

## The test

`TestADoorAtRestWhoseTaskSubtreeTurnsSaysWorking`
(`internal/tui3/statuswork_test.go`), committed before the fix. It stands
the door at rest with a task node turning, asserts the word is `working`
(and accent, and on the row), asserts no spinner is drawn, and asserts a
settled roster returns it to `idle`.

## The thirteen assertions and the manual

The thirteen assertions named in the brief (`bundle_test.go`,
`chrome_test.go`, `crew_test.go`, `lifecycle_test.go`, `roommodel_test.go`,
`statusdeck_test.go`, `surface_test.go`, `tui3_test.go`) **all stay
`idle`**: every one is on a door with no task and no job running — a fresh
screen, a settled turn, a width ladder — so they are pinning `idle` for a
genuinely idle door and remain correct.

**One assertion outside the brief's list moved**, because its fixture is a
door at rest whose node is still turning: `roomstatus_test.go`'s
`TestTaskFooterFollowsTheWorkBeingRead` left a room whose node was admitted
`TaskRunning` and asserted the conversation's word equalled the bare run
state (`idle`). It now asserts the conversation's own reading, which while
that node turns is `working`.

The manual is brought into agreement at
`internal/manual/chat/screen.md` — a row for the at-rest `working` in the
state word's own table, and the sentence that used to call the conversation
"idle" while a node worked — and
`internal/manual/chat/reading-a-task-page.md`, whose "because your
conversation is idle" is now "because your conversation's own turn has
ended". `internal/manual/chat/empty-screen.md`'s two lines stay `idle`: a
fresh conversation has handed nothing out, so there is no subtree to be
working.
