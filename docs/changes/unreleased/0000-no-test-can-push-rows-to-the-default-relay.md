---
kind: internal
title: No test in cmd/codeaf can push pool rows to the default relay
pr: 0000
surface: [engine]
invalidates: []
---

Two sweep tests in `cmd/codeaf` recorded judged pool rows and pinned no
submit address, so the push each recorded landing runs resolved the
relay's own address — the one `poolcfg` holds whenever nothing names a
submit address — and sent the fixture's scores to the public pool on
every run of the package's tests.

- `TestMain` now pins `CODEAF_MODEL_POOL_SUBMIT_URL` to a dead loopback
  (`http://127.0.0.1:1/v1/rows`) unless the environment deliberately
  named one, the same floor the call log and the state root already
  stand on. Rows a test built never reach the pool; a test that means it
  still pins its own with `t.Setenv`, the way the pool tests'
  neighbours do.
- The two sweep tests pin the dead loopback beside their other pins and
  read the outbox back after the sweep: every row the sweep recorded is
  still pending there, none sent.
- A new `pool_guard_test.go` resolves the pool's config the way the
  binary resolves one under the package's test environment, with no pin
  of its own, and fails when the submit address names the relay's own or
  any `https://` address — so a test that forgets its pin fails instead
  of sending.
