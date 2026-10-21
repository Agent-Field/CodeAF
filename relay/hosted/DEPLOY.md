# Hosted relay: staging first, then production

The owner performs account operations. This lane ran local Workers only: no login,
deployment, account request or production request. Commands marked **account only**
cannot be proved by `wrangler dev`. Even `deploy --dry-run` is left to the owner,
because the lane instructions forbid invoking `deploy`.

## Prerequisites and permissions

- Node 22.14 or newer (the local proof used 22.14; tests use `node:sqlite`), npm, the Go version in `go.mod`,
  Python 3, curl and ripgrep. Use the lockfile's Wrangler, not a global installation.
- A Cloudflare account on Workers Paid, with R2 enabled, and the `agentfield.ai`
  zone in that account. Confirm that the existing proxied DNS record for
  `codeaf.agentfield.ai` is correct; this runbook does not replace it.
- An API token restricted to that account and zone. Deployment needs account
  **Workers Scripts: Edit** (including Durable Object migrations), account
  **Workers R2 Storage: Edit** for creating the bucket, zone **Workers Routes: Edit**,
  and zone **Zone: Read** for resolving `zone_name`. Account discovery may also need
  **Account Settings: Read**; set the account id explicitly. Billing queries need
  account **Account Analytics: Read**, which may be a separate read-only token.
  Do not paste tokens into files, arguments, shell history or this repository.
- Before an upgrade, retain the previous deployed version id and check that its
  code can still read the current SQLite tables. A code rollback cannot undo data
  migrations. The global budget uses the existing `NEWCOMERS` namespace; no new
  Durable Object namespace is introduced by these limits.

These are **separate config files**, not named Wrangler environments. Use
`-c staging.toml` and `-c production.toml`, without `--env staging`.

| Config | Worker | R2 bucket | Public entry |
| --- | --- | --- | --- |
| `staging.toml` | `caf-relay-staging` | `caf-relay-staging-frames` | its account's workers.dev hostname, wire at root |
| `production.toml` | `caf-fabric` | `caf-fabric-frames` | `codeaf.agentfield.ai/fabric*` and `codeaf.agentfield.ai/p/*` |

Both configs bind `IDENTITY`, `PAIR_GATE`, `MAILBOX`, `NEWCOMERS`, `LINK_GATE`,
`LINK_REQUEST` and `FRAMES`. Migrations `v1` and `v2` create the namespaces on the
first deploy. Production disables workers.dev, preview URLs and observability.
Staging enables the test proxy header and short timing/rate overrides; production
must keep `TRUST_PROXY` absent. Never copy staging's `CAF_LIMITS` wholesale into
production. `LEASE_POLICY=amended` stays the same on both.

## Local rehearsal (executed)

From `relay/hosted`:

```sh
HOME=/proc sh -c 'npm ci --no-audit --no-fund && npm test'
sh test/deploy-local.sh
```

`HOME=/proc` is a read-only-home proof on Linux; ordinary `npm test` uses the same
hermetic wrapper on other systems. `test/local.sh` supplies temporary config,
cache, registry and log directories and disables Wrangler metrics, banners and
skills update prompts. It never repurposes the caller's home. Dependencies are
installed before the test; the test itself uses local tools. The project `.npmrc`
keeps npm's own cache and logs under the ignored `.wrangler/npm-cache` directory.

`test/deploy-local.sh` starts the **production config locally**, with its own name,
R2 directory and `--inspector-port 0`, checks the `/p/k7m2q9xd` HTML page and a
`404 {"err":"not_found"}` beneath `/fabric`, then stops it. It starts a fresh local
Worker with small global cost ceilings and proves admission, storage, existing
reads, retries and owner erasure over signed HTTP. Finally it starts the **staging
config locally** through the canonical conformance runner, including the second
two-mailbox relay. Processes are cleaned up on exit; failed runs keep their temporary
evidence directory and print its path. This exercises the actual configs and wire;
DNS, account permissions, migrations on a remote account and billed usage remain
account-only checks.

For a persistent local stand-in, the equivalent launch is:

```sh
sh test/local.sh node_modules/.bin/wrangler dev --local -c production.toml \
  --name caf-production-local --ip 127.0.0.1 --port 8787 \
  --inspector-port 0 --persist-to /tmp/caf-production-local
```

The automated rehearsal executed that launch shape with an allocated free port,
a unique name and a temporary persistence directory. It ran these smoke checks
against its loopback URL; substitute the remote base only at the account step:

```sh
curl -fsS http://127.0.0.1:8787/p/k7m2q9xd
curl -si http://127.0.0.1:8787/fabric/does-not-exist
```

For the complete two-relay matrix, from the repository root:

```sh
GOFLAGS=-buildvcs=false GOMAXPROCS=4 \
  CODEAF_FURROW=/path/to/the/approved/furrow-engine \
  FURROW_ARTIFACT=/path/to/the/approved/furrow-engine \
  SKIP_E2E=1 bash scripts/relay-conformance.sh
```

This starts `bin/codeaf relay --listen <loopback> --store <temporary directory>`
after `make build`, and two uniquely named local Workers, including a two-mailbox
Worker for `RelayFull`. The Go clients test both implementations. `SKIP_E2E` omits
only the separate long JS e2e script; `npm test` and all eight Go conformance
suites still run. `CONFORMANCE_WORK` chooses an evidence directory and
`CONFORMANCE_TRACKS=go` or `worker` selects a track for diagnosis. The local lane
needed explicit `-built` engine staging because the supplied engine did not match
the current pin; NOTES.md records that build limitation. Do not ship that override.

Tracks run one after another by default, avoiding disk-heavy Go tests beside a
local Worker's timed requests. `CONFORMANCE_PARALLEL=1` opts into concurrency on
an isolated machine; `CONFORMANCE_SERIAL=1` explicitly forces sequential tracks.
`CONFORMANCE_CODEAF=/absolute/path/to/bin/codeaf`
uses a candidate already built with `make build`; `CONFORMANCE_LIVE_ONLY=1` then
runs the live Go wire track without repeating its in-process package tests. The
default matrix still includes those package tests and builds its own candidate.
`CONFORMANCE_WORKER_CONFIG` selects a config for the local Worker track; the
deployment rehearsal sets it to `staging.toml` and runs the eight suites separately.

## Staging on the real account (account only; not executed)

Use the owner's secure token environment and set `CLOUDFLARE_ACCOUNT_ID` to the
intended account. Inspect identity and the currently deployed version before writes:

```sh
node_modules/.bin/wrangler whoami
node_modules/.bin/wrangler deployments list -c staging.toml
```

For the **first deployment only**, create the bucket; on an upgrade keep it:

```sh
node_modules/.bin/wrangler r2 bucket create caf-relay-staging-frames
```

Check the bundle and bindings before publishing, then deploy staging:

```sh
node_modules/.bin/wrangler deploy -c staging.toml --dry-run
node_modules/.bin/wrangler deploy -c staging.toml
```

Read the staging URL and version id from deployment output. Set `STAGING_URL` to
that workers.dev origin (no `/fabric` prefix); set `STAGING_VERSION` to the id.
Smoke the same static page and JSON refusal that the local rehearsal exercised:

```sh
curl -fsS "$STAGING_URL/p/k7m2q9xd"
curl -si "$STAGING_URL/does-not-exist"
node_modules/.bin/wrangler deployments list -c staging.toml
node_modules/.bin/wrangler versions view "$STAGING_VERSION" -c staging.toml --json
```

Expect HTML with the strict CSP on the first request, and HTTP 404 with JSON
`{"err":"not_found"}` and `Codeaf-Now` on the second. From the repository root,
run the same tagged Go suite used by the local rehearsal:

```sh
GOFLAGS=-buildvcs=false GOMAXPROCS=4 go test -p=2 -count=1 \
  -timeout 25m -tags relayurl ./internal/relayconf/ -relay-url="$STAGING_URL"
```

This creates real staging identities and ciphertext. Read every failure and skip.
`RelayFull` needs a second, two-mailbox relay and is skipped without
`-relay-small-url`; the complete local matrix covers it. Do not point that second
URL at production. Staging supplies the 4 s mailbox TTL, 2 s minimum erasure
grace and high per-address/request allowances the remaining cases need.

## Production (account only; not executed)

Proceed only after staging conformance is green. Record the previous production
version as `PREVIOUS_PRODUCTION_VERSION` before deployment. Create the production
bucket only if this is the first deployment:

```sh
node_modules/.bin/wrangler deployments list -c production.toml
node_modules/.bin/wrangler r2 bucket create caf-fabric-frames
node_modules/.bin/wrangler deploy -c production.toml --dry-run
node_modules/.bin/wrangler deploy -c production.toml
```

The dry run must name the production bucket and both existing-host routes,
`/fabric*` and `/p/*`. It must not expose the wire at the hostname root. Record
the new version id as `PRODUCTION_VERSION`, then check the exact local smoke paths
on the production host:

```sh
curl -fsS https://codeaf.agentfield.ai/p/k7m2q9xd
curl -si https://codeaf.agentfield.ai/fabric/does-not-exist
node_modules/.bin/wrangler deployments list -c production.toml
node_modules/.bin/wrangler versions view "$PRODUCTION_VERSION" -c production.toml --json
```

Expect the same HTML and JSON 404. Do not run the unrestricted conformance suite
against production: it deliberately exhausts quotas and creates many identities.

## Cost settings, billing and rollback

`src/limits.js` is the default limit table. `CAF_LIMITS` can override
`globalNewIdentitiesPerDay` (10,000) and `globalStoreBytes` (1 TiB). A zero value
closes the corresponding door. Changing these vars is an account operation and
follows the same staging/dry-run/deploy order; it needs no code change. See README
for rolling-day admission, crash-conservative storage reservations, initial R2
inventory and erasure. Limits bound new stored data, not all possible request
charges. Inactive-identity expiry is off and has no knob in this version.

Billing is **account only**. `cost/billed.py` reads Cloudflare's analytics API and
needs `Account Analytics: Read`; neither a local Worker nor a fabricated invoice
can prove an actual charge. Its help and 13 local Python tests were executed:

```sh
python3 cost/billed.py --help
python3 -m unittest discover -s cost -p 'test_*.py'
```

With the billing token securely supplied and actual UTC start/end times:

```sh
python3 cost/billed.py --account "$CLOUDFLARE_ACCOUNT_ID" \
  --script caf-relay-staging --bucket caf-relay-staging-frames \
  --from "$BILLING_FROM" --to "$BILLING_TO" --out /tmp/caf-staging-billed.json
python3 cost/billed.py --account "$CLOUDFLARE_ACCOUNT_ID" \
  --script caf-fabric --bucket caf-fabric-frames \
  --from "$BILLING_FROM" --to "$BILLING_TO" --out /tmp/caf-production-billed.json
```

Allow for analytics settling. The script reports usage for Worker requests/CPU,
Durable Object requests/duration/rows, and R2 operations; it is not a payment or a
change to the account. Its scope refusal explains the missing Analytics permission.
Keep the account's usage/billing dashboard beside these measurements.

To roll back **code**, choose a previously deployed version compatible with the
current stored data, then use the real account command:

```sh
node_modules/.bin/wrangler rollback "$PREVIOUS_PRODUCTION_VERSION" -c production.toml \
  --message "Restore the previous relay version"
node_modules/.bin/wrangler deployments list -c production.toml
```

Repeat the smoke checks. Wrangler's rollback, deployments-list and versions-view
command syntax was checked with local `--help`; the operations themselves were
not executed. Never delete either R2 bucket, the Worker, or any Durable Object
namespace, and never apply a migration with `deleted_classes` to roll back code.
Those objects hold people's data. Rolling back below the global-budget version
also removes its admission/storage protection; choose a compatible version or
retain the current brake implementation while fixing the fault.
