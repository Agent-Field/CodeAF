# Issue #837 — status: NOT closed. No code landed.

Branch `af/issue-837` at `e67f1827b` is **unchanged** by this run: `git status`
shows only the untracked briefing file; `go build ./...` is green on the
untouched tree. Nothing was committed, pushed, or opened. The time budget went
into reading the four files the issue names and the law around them; the write
was never reached. Below is the map that makes the next run a short one.

## The writer (already exists, uncalled)

`internal/session/task_beat.go`:
- `taskBeatRow` holds `request_started` / `request_finished`, plus `phase`,
  `started`, `requests`, `updated_at`.
- `taskBeatRow.working()` is exactly the predicate the issue asks for:
  `!RequestStarted.IsZero() && RequestFinished.Before(RequestStarted)`.
- `readTaskBeat(path) (taskBeatRow, bool)` is the whole read side, deliberately
  placed in `session` so the file's shape has one definition. **No non-test
  caller** — confirmed by the issue's grep and again by reading.
- Path: `<session folder>/tasks/<id>.beat.json`; owned by
  `taskStore.beatPath(id)` (end of task_beat.go) and named on the record as
  `taskRecord.Beat` (`task_store.go` line 332, `Beat string json:"beat,omitempty"`),
  so a reader that has tasks.json never guesses a path.

## The reader that must be added — the room's facts row

`internal/tui3/room.go`:
- `roomFactsLine` (~line 2496) draws `⠙ working · 2m12s · $0.04 · 6 tool calls`.
- Its fields come from `roomFactsOf(node)` in `internal/tui3/roomfacts.go`,
  which builds `roomFactFields{state, clock, spend, calls, live, model, effort}`
  and feeds both the grouped layout and the ranked fallback. **The `live` field
  is the seam** — `roomLiveWord(node, work)` (room.go ~2660) returns
  `work.running` (the newest in-flight tool's name) or `still working`.
- `roomWorkOf(es []entry)` (~2726) walks the page's entries; the beat is a
  *different* source that tells the truth even when the page has no entry yet
  and when no phase news arrives.
- The refresh seam for a hosted room is `refreshRoomRecord(journal)` in
  `internal/tui3/roomrefresh.go` — it already calls `r.takeRequests(record.Requests)`
  after each journal read; the beat read belongs on the same tick (read once per
  refresh, cached on `taskRoom`, or the segment never updates — the "segment
  that never moves" trap named in the task).

## The #747 vocabulary to reuse verbatim

The chat foot's in-flight segment is `liveRiderAt` in `internal/tui3/render.go`
(~2552), which already routes a ROOM's node through `a.windowPhase()` /
`a.windowWorking()` (`internal/tui3/phase.go` 382–442) — the room's own rate
was fixed once already. Its spellings:
- while writing/thinking: `38 tok/s` alone (`tokenWord(int(news.Rate)) + " tok/s"`)
- other phases: `phaseFields(news, now)` → `connecting · 1.2s`,
  `first word · 3.1s → parasail at 4.4s`, `paced · retry in 6s`.
- elapsed formatter: `tookWord` (`timestamps.go` ~323: `0.4s`, `12s`, `2m12s`)
  and `countUpWord` (`toolview.go` ~1398) — one shared source of truth for
  "how long", never a second spelling.

The issue's own sentence is: while `request_started` is newer than
`request_finished`, the header's live segment shows the open CALL's elapsed
and its live token rate, spelled exactly as the foot spells them; when the
call ends it reverts to the task's own elapsed. Reuse the foot's formatter
(lift into one shared function both call) — re-typing the sentence drifts.

## What the acceptance asks, mapped to files

- **Unit:** a `tui3` render test feeding a beat row with `request_started`
  after `request_finished` draws the segment; the reverse draws none.
- **e2e:** `internal/e2e/tuiwords_test.go` holds every needle; the tmux test
  opens a task against a stub whose node holds one call open while streaming,
  asserts the segment during and its absence after. The #747 e2e's slow-stream
  stub is the pattern to copy.
- **Manual:** `internal/manual/chat/reading-a-task-page.md` is the task-room
  page (same commit; never machinery words).
- **Changelog:** `make changelog-new PR=837 KIND=fix SLUG=<slug>`, one source
  of truth, invalidates: line.

## Checks worth knowing before starting

- `go build ./...` green at `e67f1827b` on `af/issue-837`.
- `gh issue view 837 --comments` returns **zero bytes** in this environment —
  the issue body came from the briefing, which matches; no comment content
  was ever seen.
- Verify with `make test-focus PKGS=./internal/tui3 RUN=<one test>` while
  working; then `go vet`, `make test-touched`; e2e last.
