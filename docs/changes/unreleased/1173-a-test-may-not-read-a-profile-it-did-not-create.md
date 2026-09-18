---
kind: fixed
title: "internal/tui3: a test may not read a profile it did not create"
pr: 1173
surface: [chat]
invalidates:
  - "`newTestApp` and seventeen other helpers in `internal/tui3` built their surface with no profile directory, so every test that used one read and wrote the ONE state root `TestMain` pins for the package run and an earlier test's `setup_seen_at` decided what a later one saw — this had already hidden a red for a week (TestTheOpeningHintNamesBothDoors green in the package run, red alone, from #680). Each app-building helper now gives its app a profile of its own."
---

The test floor points an empty profile directory at one shared state root for the
whole run (`tui3_test.go`'s `runTests`), and `internal/home`'s `resolve` prefers
`CODEAF_HOME` over `HOME` — so a bare `newTestApp`, which named no profile,
resolved under that one root and shared it with every other bare app in the run.
The fix is at the helpers, not the call sites.

## What the helpers do now

- `newTestApp` (663 call sites) and `benchApp` (9) have no `*testing.T` to call
  `t.TempDir()` through, so they mint a directory with `mintProfileDir()` — a
  fresh subdirectory of the run's own `CODEAF_HOME`, thrown away with the run.
  A sweep of the 663 call sites to thread a `t` was avoided on purpose, as the
  brief asked: another branch carries `internal/tui3` changes and would collide.
- The sixteen other helpers that take a `t` name `ProfileDir: t.TempDir()`:
  attachLab, mixedLab, newRewindApp, modalLab, browseLab, fileLab, asyncApp,
  hostLab, welcomeApp, recallApp, memoryPlaceApp, hostedSurface, exportLab,
  folderLab, aliasApp, completionApp.
- `newTestAppWithProfile(dir, agent)` is the escape hatch for a test that means
  two surfaces to share ONE profile. It is used by
  `TestAnOrdinaryLaunchCarriesTheCrewOnItsPageAtEveryWidth` (crew_test.go), which
  seeds a profile with `config.ApplyCrew` and has two surfaces read it.

## Tests that legitimately depend on a shared root

**None found.** `go test ./internal/tui3` with every helper now isolating its app
produced exactly one failure, and it was not a shared-root dependence: it was
`TestAnEmptyProfileDirectoryIsTheOrdinaryProfileAndStillHasACrew` (crew_test.go),
whose subject IS the launch that names no profile — it asserted
`a.profileDir == ""` and could not be built by `newTestApp` any more. It now
builds through `ordinaryLaunch`, which names no profile and still pins a state
root of the test's own.

The tests that DO carry a setting from one surface to another already name the
directory themselves, so nothing had to be shared implicitly: e.g.
`TestTheColumnsPostureIsRememberedAcrossSessions` (railaway_test.go:220),
`TestChoosingAModelWritesItWhereTheNextLaunchReadsIt` (modelmemory_test.go:17),
and `noticeApp`/`sheetApp` (notice_test.go:366, chrome_test.go:25).

## Still exposed, and left alone

The law is about helpers. The brief narrowed it that way, so 45 direct `newApp(`
calls inside test and benchmark bodies still name no profile and still share the
run's root (measured: 62 direct calls in test bodies, 17 naming a `ProfileDir`).
They are a call-site sweep this brief forbade; a future change that widens the
law to test bodies would need to give each one an `ordinaryLaunch`-style root.

## Verification

- `go test ./internal/tui3 -count=1 -timeout 600s` — green (once the crew test
  above was moved to `ordinaryLaunch`); the first run reported that one failure
  and nothing else.
- `go test ./internal/tui3 -run '^TestTheOpeningHintNamesBothDoors$' -count=1` —
  green on its own, which is the whole point: it no longer depends on a marker a
  test before it happened to write.
- `go test ./internal/tui3 -run '^TestEveryHelperThatBuildsASurfaceNamesAProfile$'`
  — green: the structural law reads this package's `_test.go` files with go/ast
  and fails a helper that builds a surface without naming a profile or moving the
  state root.
