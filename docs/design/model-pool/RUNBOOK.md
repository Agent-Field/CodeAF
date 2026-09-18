# Model Pool relay — runbook

The relay is a Cloudflare Worker under `relay/`. It accepts measurement rows
from installs over NDJSON at `POST /pool/v1/rows`, folds each install's rows
into per-install running totals ("sheets") in KV, and once an hour publishes a
signed, versioned JSON index that installs read. It serves, all under the
`/pool/` prefix:

- `POST /pool/v1/rows` — submit rows. Requires the `X-Codeaf-Install: <32
  lowercase hex>` header; the body is at most 256 KiB and at most 200 lines.
- `GET /pool/index.json` — the published index document.
- `GET /pool/index.json.sig` — its detached Ed25519 signature.
- `GET /pool/healthz` — a liveness probe.

Every other path answers 404 with an empty body. The document is signed with
Ed25519: the Worker holds the 32-byte private seed as the secret
`POOL_SIGNING_KEY` (standard base64) and the 32-byte public key as the plain var
`POOL_PUBLIC_KEY` (standard base64). Installs read
`https://codeaf.agentfield.ai/pool` by default; `CODEAF_MODEL_POOL_RELAY_URL`
points an install elsewhere and `CODEAF_MODEL_POOL_PUBLIC_KEY` sets the trusted
key.

## Deploy

1. Have a Cloudflare account with **Workers & Pages** enabled.
2. `npm i -g wrangler && wrangler login`.
3. `cd relay && wrangler kv namespace create POOL`, then paste the returned id
   into the `[[kv_namespaces]] id` in `wrangler.toml`.
4. Generate the signing key once, off the repository, and set the secret:
   - `openssl genpkey -algorithm ed25519 -out pool-ed25519.pem`
   - raw 32-byte seed, standard base64:
     `openssl pkey -in pool-ed25519.pem -outform DER | tail -c 32 | base64`
   - raw 32-byte public key, standard base64:
     `openssl pkey -in pool-ed25519.pem -pubout -outform DER | tail -c 32 | base64`
   - `wrangler secret put POOL_SIGNING_KEY` reads the seed from stdin, so pipe
     it in:
     `openssl pkey -in pool-ed25519.pem -outform DER | tail -c 32 | base64 | wrangler secret put POOL_SIGNING_KEY`
   - put the public key into `[vars] POOL_PUBLIC_KEY` in `wrangler.toml`.
   Never write the seed anywhere in the repository.
5. `wrangler deploy`.
6. DNS and the route: create a proxied DNS record for `codeaf.agentfield.ai`
   (AAAA `100::` or A `192.0.2.1`, proxied) so the route can attach. The route
   in `wrangler.toml` binds `codeaf.agentfield.ai/pool/*` to the Worker.
   Installs read `https://codeaf.agentfield.ai/pool` by default, and
   `CODEAF_MODEL_POOL_RELAY_URL` points an install elsewhere.
7. First publication: `wrangler triggers deploy` and wait for the next `:17`,
   or, under `wrangler dev --test-scheduled`, drive it by hand with
   `curl -X POST "http://localhost:8787/__scheduled"`. Publishing does not
   depend on the cron: the first read past PUBLISH_EVERY republishes in the
   background while serving what is stored, so the cron is a floor rather
   than a requirement, and step 7a below is only for a store you want filled
   before anyone reads.
7a. If the store is still empty forty minutes after the first `:17` (a fresh
   Worker's cron can miss its first tick), trigger the scheduled handler once
   by hand: `wrangler dev --test-scheduled` bound to the remote namespace, then
   `curl "http://localhost:8787/__scheduled?cron=17+*+*+*+*"`; the next `:17`
   takes over from there. The same hand trigger is how to publish at once
   after loading rows (`relay/tools/prime.py`, or any large batch): a read
   republishes only when the stored document is older than PUBLISH_EVERY, so
   fresh rows behind a young document wait up to an hour otherwise.
8. Verify from a laptop: `curl -sI https://codeaf.agentfield.ai/pool/index.json`
   (expect 200 and an ETag), then `codeaf pool verify`, then `codeaf pool
   status`.
9. Know what the store holds — one running total per install, cell and day; a
   per-install per-day quota counter; the nonces it has folded for that install
   and day (`seen/<install>/<day>`, one week's TTL); and the last index document
   and its signature — and how to wipe it: `wrangler kv key list` to see the
   keys and `wrangler kv key delete <key>` to drop one.

   What a batch costs the meter: one KV read and one KV write for each distinct
   (day, cell) it folds into, one read per day the batch carries (that day's
   seen set), and one read and one write per day it has fresh rows on (that
   day's seen set and its quota counter) — neither scaling with the row count,
   so a 200-row batch over ten cells on one day is 12 writes where one write per
   row was 200. The relay is deployed on Cloudflare Workers' `plan: <operator
   fills in>` plan; neither this runbook nor `wrangler.toml` names one, so fill
   it in. Cloudflare's free tier allows 1,000 KV writes and 100,000 KV reads per
   day (a paid plan is metered, not capped). The day's seen set is at most
   `ROWS_PER_INSTALL_PER_DAY` nonces of 32 hex characters plus a separator —
   16,500 bytes at the default 500, against KV's 25 MiB value limit — so a plan
   that raises the quota past what one value holds is refused rather than
   allowed to drop nonces.
10. Rotating the signing key: generate a new seed, add the new public key to the
    binary's trusted key list, then redeploy with the new secret.
11. Keep the seed index the binary carries faithful to the relay. Before a
    release, run `go run ./internal/pool/index/cmd/seedgen -check` from the
    repository root: it fetches the signed index, verifies it, and diffs it
    against `internal/pool/index/seed.json` and
    `docs/design/model-pool/data/seed-cells.csv`, exiting non-zero when either
    has drifted. Review the diff, then re-run without `-check` to write both,
    and let the regenerated files ride the release's pull request and change
    note. The command checks signatures under the key the build carries; pass
    `-key <base64>` (repeatable) for a relay of your own, and `-url`/`-mirror`
    to point it elsewhere.

## Observability

`wrangler.toml` carries the dashboard's observability settings (invocation
logs and traces, persisted, sampled at 100%) so a redeploy keeps them. The
Worker writes no log line of its own; what the platform persists is its
invocation record: route, method, status, timing and request metadata, which
includes the caller's network address and the `X-Codeaf-Install` header for
the platform's retention period. That record is the one place a row can be
tied to an address. A relay that should keep none sets `persist = false` or
`head_sampling_rate = 0` in both blocks.

Judge severity and the primed rows. The index's `role_quality` cells come
out of a per-judge severity fit (`relay/src/sheet.js`): every judge's scores
are shifted by a fitted β before they are pooled, so a judge who scores high
or low on everything is taken out of the published means. The priming script
(`relay/tools/prime.py`) posts every seat as exactly 0 or 100 under one judge
id, so in that fit the primed judge's rows carry only the two ends of the
rubric. For a judge whose rows are all primed, with `n_prime` rows on cells
the other judges hold `n_other` rows of at weighted mean `c`, the stationary
fit puts its severity at `β = n_other · (x̄ − c) / (n_prime + n_other)`, where
`x̄` is the primed rows' own mean (100 × the pass rate), and moves the centre
of every cell it shares by `n_prime / (n_prime + n_other)` of the gap toward
`x̄`. As a worked number: `n_prime = 385` rows, all failures (`x̄ = 0`),
against `n_other = 600` rows at `c = 88` gives
`β = 600 · (0 − 88) / 985 ≈ −53.6` — and the fit holds every severity inside
±10 (a judge further off than a tenth of the rubric's width is a different
rubric, not a severity), with each adjusted score clamped back into
[0, 100], so a cell that judge scores alone shifts by at most 10 points
either way. An operator weighing a purge can read the size of the pull from
the same formula with their own `n_prime` and `n_other`.

## Run your own relay

Generate your own keypair with the same openssl lines as step 4, then
`wrangler deploy` to your own account with your own route or the workers.dev
address, `wrangler secret put POOL_SIGNING_KEY` to set the seed, and your own
`POOL_PUBLIC_KEY` in your own `wrangler.toml`. Point installs at it with
`CODEAF_MODEL_POOL_RELAY_URL` and `CODEAF_MODEL_POOL_PUBLIC_KEY` (the trusted
key).

The mirror workflow is optional; it takes the same two values as its workflow
env. `relay/` holds no secret and no value that is not documented in
`wrangler.toml` or this runbook — the quota, the min installs and the cron — so
a copy of the directory is a complete relay.

## Repository settings, for the optional mirror

- **Actions:** enable Actions on the repository if it is disabled.
- **Workflow permissions:** under Settings → Actions → General → Workflow
  permissions, set "Read and write", which the mirror needs to push the data
  branch.
- **Run it once by hand:** the workflow can be dispatched only once its file
  is on the default branch, so the first run waits for the branch that carries
  it to merge; until then the mirror address answers 404, which the client
  treats as one more unreachable mirror and reads its cache or the seed.
  Then dispatch the mirror workflow with
  `workflow_dispatch`.
- **Confirm it worked:** a commit appears on the `model-pool` branch.
