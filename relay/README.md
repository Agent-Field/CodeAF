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
