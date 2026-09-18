---
kind: added
title: Three more `internal/pool` packages — an outbox, a settings resolver and a signed-document fetcher
pr: 1093
surface: [engine]
invalidates: []
---

Standard library only, and nothing on a surface wired to them yet.

- `internal/pool/outbox` keeps small measurement rows in a local
  newline-delimited JSON file and hands them to a destination in batches, over
  http or into a file. A row that arrived is marked sent and the mark outlives
  the handle, so nothing is delivered twice; a batch that did not arrive leaves
  its rows and every row after it pending, with an error naming what failed.
  The file is capped from the old end, the payload is stored compacted, and a
  caller never has a full disk or a dead destination raised at it as anything
  but a returned error.
- `internal/pool/poolcfg` resolves how the pool behaves from one stored setting
  and an environment handed in as a function, so there is no clock, no disk and
  no reading of the process environment in it. Every field carries where its
  value came from — `default`, `setting`, `env` or `ci` — and the addresses it
  accepts are https, http on loopback only, `file://` and absolute paths.
- `internal/pool/pull` fetches a JSON document, checks a detached ed25519
  signature at the same location with `.sig` appended, and keeps the last good
  copy under a cache directory so a run that cannot reach the source still has
  an answer. A young cache skips the network entirely, an ETag turns into a
  conditional request, a version lower than the cached one is refused, and every
  failure hands back the cached copy beside a non-nil error.
