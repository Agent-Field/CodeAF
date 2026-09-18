---
kind: fixed
title: cmd/codeaf's test floor clears CODEAF_PROFILE_DIR, covering the whole package
pr: 1146
surface: [chat, build]
invalidates:
  - "The cmd/codeaf test floor was believed to isolate every variable that decides where a test writes. It moved HOME and CODEAF_HOME but answered an exported CODEAF_PROFILE_DIR as a deliberate pin and left it alone; the floor now clears it unconditionally, so no test in the package inherits a profile the environment named."
  - "The four per-test CODEAF_PROFILE_DIR pins from #1145 were what kept a test's writes out of an inherited profile. They still hold but are now belt and braces: the package floor clears the variable before any test runs, and deleting the pins is a separate decision."
---

#1145 fixed one test at a time and proposed the class-stopper without doing it:
`isolateTestEnvironment` — the floor `TestMain` gives every test file in
`cmd/codeaf` — pins HOME and CODEAF_HOME through `pinTestEnv`, and `pinTestEnv`
leaves any variable the process was deliberately started with. An exported
`CODEAF_PROFILE_DIR` is precisely that case, and it is the variable that decides
where the package's writes go (`config.ProfilePath` answers it before the state
root), so a harness exporting it at a live profile reached the whole package
however far the roots were moved. Routing the profile through `pinTestEnv` would
have been a no-op in exactly that case, so the floor clears it unconditionally —
the way `internal/tui3`'s TestMain already does — and restores whatever it
cleared when the run ends.

The floor is held down by `testfloor_test.go`, which runs a child of the test
binary with the variable exported at an empty stand-in, has the child take the
real fault road and assert what the floor left it, and reads the stand-in back.
It failed before the fix and passes after, and the measurement behind that is
the whole package rather than one road: `go test ./cmd/codeaf -count=1` with
`CODEAF_PROFILE_DIR` exported at an empty stand-in left nine entries in it on
the pre-change floor — `chat.log`, `model-catalog.json`, and a `pool/`
directory holding the six files of the pool's start-up errands #1145 measured
in one test — while every other test in the package passed. That is why no
road in this package can see the leak and a floor guard has to. The same run on
this tree passes in 89s and leaves the stand-in empty. No real profile is
touched by any of it, not even to check it.

Now redundant under the floor, and deliberately left in place: the four per-test
pins from #1145 — `nopanic_test.go`'s two reportFault tests, `telemetry_test.go`'s
`telemetryHome`, `chatv3_belt_test.go`'s door walk, and `vocabulary_test.go`'s
shorthand test. They cost one `t.Setenv` each and keep each test true on its own
if the floor is ever lifted.
