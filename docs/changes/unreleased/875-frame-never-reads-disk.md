---
kind: fixed
title: the frame reads a memo and never the disk, and a go/ast law keeps it that way
pr: 875
surface: [chat, docs]
invalidates:
  - "The frame stat'd every visible picture on every paint. `(*app).picture` took an `os.Stat` before the cache lookup because the file's modification time and size WERE the preview cache's key — twelve open pictures at 30 Hz was 360 syscalls a second, taken from inside `View`. The key is the same key; what is in it is now a fact the LOOP learned (`internal/tui3/learned.go`), read at `open`, on the pulse's ten-second beat, and at each arrival — the picture call ending, the file being attached, the mirror's copy landing, the press that expands a collapsed picture."
  - "`/workspace <path>` probed git ON the loop. It called `a.gitProbe(resolved)` straight, which is `git rev-parse` and `git status` under one shared 400 ms ceiling, so the surface froze for up to four tenths of a second on the keystroke that set the workspace. It takes `probeGit` now — the off-loop road every other reading of the repository already took — and the branch arrives as a `gitMsg`, so the legend draws nothing for a moment rather than everything freezing."
  - "`CachedModels`/`CachedModelsFor` were read straight from the render path. Every frame that had to name a model with no catalog behind it — the first-run screen, every window opened with no key — re-read `~/.aforge/v3/models.json` in full. The frame reads `app.cachedModelsFor`, a memo filled at `open` (`app.learnModelLists`), refreshed on the beat, and dropped by name when ctrl+r's fetch or a service connection rewrites that file."
  - "There was no check on ARCHITECTURE.md's fourth law. `internal/tui3/framedisk_law_test.go` now walks this package with go/ast from `(*app).View` — through calls on the surface, package functions, and func-typed FIELDS by what is assigned to them — and fails on `os.Stat`, `os.ReadFile`, `os.Open`, `exec.Command`, `filepath.Glob` or the network. A sibling asks the update loop the narrower question (no process, no wire, on the loop) and walks every closure except the ones handed off as commands. The allowlist is one entry: the picture decode."
  - "A job's page took its log's first reading inside the frame (`jobPageSeed`, one `os.Open` per page open, from the layout). It is deleted: `showJobPage` has one caller, `openJobPage`, and that caller has always followed it with `jobPageArm`, which opens the same file off the loop and starts the beat that keeps it moving."
  - "A bare notice board went through the ledger LOADER to conclude it had no ledger. `noticeEvent`'s fallback mints one (`bareNoticeBoard`) instead, so no file read is on the graph of what a frame can reach."
  - "A file rewritten behind a running window is noticed on the pulse's beat rather than by the next frame. Nothing on a machine tells a terminal that a png was overwritten or that another process rewrote the model cache; the frame no longer asks, so the ten-second beat and the arrival that changed the bytes are the two ways it is learned. A test that rewrites one of those files mid-run has to say so (`a.refreshLearning()`), which is what the beat stands in for."
---

Three readings, one shape: *a fact read off the disk under a name, kept until
something says the bytes moved*. So there is ONE mechanism for it — `learned[T]`
— with three doors and a rule about which loop may open each: `of` is the
frame's and reads nothing, `learn` is the loop's and is called by `open`, by a
tick and by an arrival, `refresh` is the beat. `catchUp` reads whatever the last
frame asked about, once, on the loop, which makes the arrival hooks a matter of
latency rather than of correctness.

Measured on `BenchmarkFramePictures` (new): a dozen expanded pictures on a
100×40 frame, M3 Max, 3000× × 5 — **328 µs** a frame against a **360 µs** median
and a **528 µs** syscall-stall tail before.
