# Model Pool relay

This Worker stores one running total per install, cell and day — the per-install
sheets it folds submitted rows into — plus a per-install per-day quota counter,
and the last published index document and its signature.

It never stores a client address and keeps no request log, and it reads the
32-byte private signing seed only from the `POOL_SIGNING_KEY` secret, which
lives in Cloudflare, not in this directory.

It serves, all under `/pool/`: `POST /pool/v1/rows` for submissions,
`GET /pool/index.json` for the published index, `GET /pool/index.json.sig` for
its signature, and `GET /pool/healthz`; every other path answers 404 with an
empty body.

A submitted row is one NDJSON line per scored seat, and its `door` is the door
the run came in by — one of `task`, `do`, `exec` or `run`. A door the wire does
not know is refused.

## Priming

The first published index was primed once, from the same measured runs the
binary's embedded seed encodes, so a machine that has never fetched a document
and one that has read the first published one carry the same numbers.

`tools/prime.py` is that one-off. Run by the operator over the private run
ledgers, which never enter this repository and stay outside it, it rebuilds the
ordinary rows those runs imply — one row per seat a run scored, on the same
wire, with the same seat and score mapping the seed was folded from — and
submits them to `POST /v1/rows`. Its `--dry-run` prints the rows and checks them
against the seed without sending anything.

It gives every originating session one install id — the first 32 hex characters
of the hash of the session name — so a session's runs fold under one install the
same way they would have had that session submitted them itself. Every row it
sends carries the judge id `codeaf/reviewer`: these runs were scored by review
rather than by a judge model, and one judge id keeps the relay's judge-severity
fit neutral.

Nothing is counted twice. A client holds one index document at a time, and the
reader takes whichever of the fetched document and the embedded seed is newer by
its generated date rather than summing them, so a row the relay counts once is
already the seed's cell and not a second observation on top of it.
