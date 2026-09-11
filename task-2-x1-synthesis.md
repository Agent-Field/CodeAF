# Issue #837 — synthesis: the seam is still absent, and here is exactly where it goes

Measured at `af/issue-837` = `e67f1827b` (= `dev` tip), shared worktree at
`/home/santosh/af-i837`. `git status` holds only another session's uncommitted
export-rename of `internal/session/task_beat.go` / `task_beat_test.go`
(`ReadTaskBeat`, `TaskBeatRow`, `Working` — the exported names the surface needs)
plus untracked `BRIEF.md` and `task-2-close-github-issue-837.md`. Nothing of
#837 is committed anywhere on the branch; the pickaxe over `internal/tui3` and
`internal/e2e` for those names is empty. The bug is open at the tip.

## The seam, anchored (all verified by reading this tree)

1. **Reader call, each refresh tick** — `internal/tui3/roomrefresh.go`,
   `refreshRoomRecord(journal []byte)`, which ends with
   `r.takeRequests(record.Requests)` (line ~77). Add after it, still inside the
   guard `bytes.Equal(r.journal, journal)` early-return: read the pulse once and
   cache it on `taskRoom` (a `beat session.TaskBeatRow` field), because the
   header is drawn on the frame and must not hit the disk per field.
   The path: `refreshRoomRecord` only holds journal *bytes*; the room's node
   journal *path* is `a.tasks[a.room.id].transcript`. The store owns the name
   (`taskStore.beatPath`, task_beat.go) as `<dir of tasks.json>/tasks/<id>.beat.json`,
   which is also **the journal's own directory** + `/<id>.beat.json`. Do not
   re-derive it in tui3: export one helper from `internal/session` (beside
   `ReadTaskBeat`) taking the journal path and the node id, so the naming has
   one definition.
2. **Draw** — `internal/tui3/roomfacts.go`, `roomFactsOf`, field `live`
   (`rowSay(a.roomLiveWord(node, work))`), drawn by both `ranked()` and
   `roomGroupedFacts`. In `roomLiveWord` (room.go ~2706): when
   `node.state == session.TaskRunning` and the cached `beat.Working()`, the
   live segment becomes the open CALL's own elapsed — `tookWord(a.now().Sub(beat.RequestStarted))`
   (timestamps.go:323, the foot's spelling) — and the room reverts to the task's
   own clock (`roomClock`) when the call ends. **A rate is not drawn off the
   beat**: `TaskBeatRow` carries `Requests` and the two edges only, no token
   count — the live `tok/s` a room already draws comes from `PhaseNews.Rate`
   through `windowPhase`/`roomPhase` (phase.go 382–442, already routed to the
   room's node). `38 tok/s` is spelled `tokenWord(int(news.Rate)) + " tok/s"`
   (render.go ~2456, `liveRiderAt`). Reuse those; never re-type them.
3. **Unit test** — a `tui3` render test feeding the cached beat with
   `RequestStarted` after `RequestFinished` draws the segment; the reverse
   order draws none. No such test exists.
4. **e2e** — `internal/e2e/tuiwords_test.go` has **no `tok/s` needle at all
   today** (grep is empty), so the new needle(s) are additions, and the
   manual law's twin (`TestEveryWordTheTmuxSuiteWaitsForStillStandsInTheSurface`)
   must find them in the surface. The tmux suite must open a task whose stub
   node holds one call open while streaming.
5. **Manual** — `internal/manual/chat/reading-a-task-page.md`, one sentence on
   what the open-call segment means, same commit, no machinery words (never
   "beat", "sidecar", "node").
6. **Changelog** — `make changelog-new PR=837 KIND=fix SLUG=<slug>`, what was
   true / what is true now / `invalidates:` line.

## What was NOT done, and why

The wall clock ran out during recon of a seam that does not exist at all.
Nothing was committed, so the tree a person reads is clean of half-work; the
uncommitted session-package rename belongs to another worker and was left
untouched, per the shared-worktree hazard both prior runs recorded.

`gh issue view 837 --comments` prints nothing in this environment (known).

## The readings the last pass reported as failing — now green

A later lane at the same tip ran them: the three tuiwords gates
(`TestEveryLaneAsksForItsKeyTheWayTheProductDoes`,
`TestEveryWordInTheTableIsWaitedForBySomething`,
`TestEveryWordTheTmuxSuiteWaitsForStillStandsInTheSurface`), the whole untagged
`internal/e2e` package, and the three `internal/plan` tests all pass at
`e67f1827b`. That failing-readings list is settled; the only thing left is the
seam. The gates still owed once it lands: `go build ./...`, `go vet` on
`internal/tui3` and `internal/session`, `make test-touched`, and the new e2e
test by name.
