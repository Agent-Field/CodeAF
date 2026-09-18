---
kind: fixed
title: a fault test writes its crash fixture only into a profile it owns
pr: 1145
surface: [chat, build]
invalidates:
  - "Tests in cmd/codeaf moved HOME and CODEAF_HOME to directories of their own but left CODEAF_PROFILE_DIR alone, and reportFault appends through config.ProfilePath(config.ProfileDir(), \"chat.log\"). A harness that exports CODEAF_PROFILE_DIR at a live profile therefore sent the fixture — `slice bounds out of range [:-1]` — into somebody's real chat.log, where it read as a genuine crash. Every test and helper that writes through a profile path now pins CODEAF_PROFILE_DIR as well."
  - "Moving CODEAF_HOME was enough to keep a test's writes out of a real profile. It is not: config.ProfilePath answers an exported CODEAF_PROFILE_DIR before it falls back to the state root, and the pool's start-up errands follow it, so a test that seats a launch mints `pool/install` and opens `pool/outbox.jsonl` in the inherited profile however far the state root was moved."
---

The fault log follows CODEAF_PROFILE_DIR and not HOME or CODEAF_HOME, so a test
that pins only the latter two still writes into whatever profile the environment
names. `nopanic_test.go` and `telemetry_test.go`'s `telemetryHome` — the second
writes the profile's own telemetry row through `config.WriteTelemetry` — were the
two tests in this class found by name; `chatv3_belt_test.go` writes the model
catalog and the pool's start-up errands through the same resolution. All three
now name every variable that decides where their writes go.

READING FOUND THREE AND MISSING ONE. The rest of the audit ran the roads instead:
every `cmd/codeaf` test that seats a launch or writes a profile path was run with
CODEAF_PROFILE_DIR pointed at an empty directory, and the directory was then
read. That measured a fourth leak — `vocabulary_test.go`'s shorthand test seats
`do` and `exec`, and the pool's errands it starts left `pool/install` and
`pool/outbox.jsonl` in the inherited profile on every run. It is fixed the same
way. The other launch-seating rows in that file stopped at a missing key before
any errand started and wrote nothing across three runs each.

`faultprofile_test.go` holds the fault case down from the outside, because a test
that pins the variable itself cannot fail on the tree that leaked: it runs the
fault test as a child process under an inherited profile and reads the stand-in.
It failed before this change and passes after it.

WHAT WOULD STOP THE CLASS, PROPOSED AND NOT DONE. `cmd/codeaf`'s TestMain already
gives the package a floor — `isolateTestEnvironment` in `testenv_test.go` — and
that floor pins HOME and CODEAF_HOME but not CODEAF_PROFILE_DIR. `pinTestEnv`
also leaves alone any variable the process was deliberately started with, so a
harness that exports a profile is precisely the case the floor does not reach.
Clearing CODEAF_PROFILE_DIR unconditionally in that helper, the way
`internal/tui3`'s TestMain already does, would cover all 170 test files in the
package at once instead of one at a time. It is not in this change: it alters
what every test here inherits, which is a wider claim than the leak needs.

