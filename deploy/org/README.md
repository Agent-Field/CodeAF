# A codeaf organization you host yourself

One `docker compose up` brings up everything an organization needs, on your own machine or
server: the **organization server** people join with their codeaf desktop app, the **AgentField**
control plane that routes work, and the reasoner nodes the factory sends work to, **SWE-AF** (builds an
issue into a pull request) and **PR-AF** (reviews one). Nothing in an image is a secret; model keys
come from your environment.

| Service | What it is | Published port | State (volume) | Runs as |
| --- | --- | --- | --- | --- |
| `codeaf` | the organization server, `codeaf factory serve` in team mode | `127.0.0.1:8787` | `codeaf-data` | uid 10001 |
| `agentfield` | the AgentField control plane (published image, pinned by digest) | `127.0.0.1:8080` | `agentfield-data` | uid 65532 |
| `runner` | the factory runner: a codeaf engine that registers the factory on AgentField as the node `codeaf-factory` | none | `runner-data` | uid 10001 |
| `swe-af` | SWE-AF, node `swe-planner`, dials AgentField over its node link | none | `swe-af-workspaces` | uid 10002 |
| `pr-af` | PR-AF, node `pr-af`, called by AgentField over HTTP | none | `pr-af-workspaces` | uid 10003 |
| `modelstub` | a scripted model, only with `--profile stub` (the keyless smoke test) | none | none | nonroot |

Every container starts as root only long enough to hand a freshly mounted volume to its user, then
drops to that user (`scripts/drop-root.sh`); nothing long-lived runs as root. Published ports listen on
127.0.0.1 until you change `CODEAF_BIND` / `AGENTFIELD_BIND` in `.env`.

## What you need

- Docker with Compose v2.24 or newer (`docker compose version`), about 4 GB of free memory and 5 GB of disk.
- For anyone who is not on the same machine: **HTTPS**. An organization server takes a password only over
  HTTPS (or from its own machine), so put a proxy that serves HTTPS in front of port 8787 (see "Other people").
- For real work: an OpenRouter key (`OPENROUTER_API_KEY`) and, to let the nodes clone private repositories and
  open pull requests, a GitHub token (`GH_TOKEN`). The smoke test needs neither.

## Start it

```sh
cd deploy/org
./setup.sh                      # writes .env with a fresh AgentField key and organization token (once)
$EDITOR .env                    # optional: OPENROUTER_API_KEY=..., GH_TOKEN=..., CODEAF_ORG_NAME=...
docker compose up -d --build    # the first build takes several minutes; later starts take seconds
docker compose ps               # every service says "healthy"
```

The first build downloads the pinned codeaf release (checked against its sha256) and builds SWE-AF and PR-AF
from pinned commits of their public repositories.

## Claim the organization, invite a second person

```sh
./operator-link.sh              # prints the operator access link (a secret; it works once)
```

On a laptop the link names `http://[::]:8787`. That is the address the server gives itself (its listen address) and a
link is only accepted with exactly that address, so it is not rewritten. A connection to `[::]` reaches the local
machine on Linux and macOS, so the desktop app on the same machine can use it; Windows cannot, and nobody on another
machine can. For them, serve the organization on a public HTTPS name (see "Other people") and the link names that.

1. Open that `codeaf://factory/join?operator=true...` link in the codeaf desktop app (or paste it into
   **Join an organization...**). Choose a username and a password. Whoever uses the link first is the operator,
   so keep it to yourself until you have.
2. From **People**, the operator makes an **invite link** for the second person. They paste it into
   **Join an organization...** on their own computer and choose their own username and password.
3. A codeaf client connects to this organization with the server's URL, which is inside the link: your public
   HTTPS name, or `http://[::]:8787` on the machine running Compose.

If the operator link is lost before anyone used it, `docker compose restart codeaf` prints a fresh one.

### Other people: HTTPS

Put any proxy that serves HTTPS in front of port 8787, then tell the server its public name and which proxy
to believe:

```sh
# .env
CODEAF_PUBLIC_DOMAIN=org.example.com          # no scheme
CODEAF_TRUSTED_PROXIES=172.16.0.0/12          # the proxy's address or range (Docker's default bridge range here)
CODEAF_BIND=127.0.0.1                         # the proxy runs on this machine; keep the port closed
```

For example Caddy, on the same host: `org.example.com { reverse_proxy 127.0.0.1:8787 }`. Links the server prints
then name `https://org.example.com`. Without `CODEAF_TRUSTED_PROXIES` the server sees every request as plain HTTP
and refuses passwords with "Passwords are only sent to this server over https".

## Real work

Put your keys in `.env` and `docker compose up -d`. `ORG_MODEL`, `SWE_DEFAULT_MODEL` and `PR_AF_MODEL` choose the
models; the defaults are inexpensive. A factory job runs on the pipeline a repository declares in
`.codeaf/factory.yaml` (a step of `kind: agentfield` with `target: swe-planner.build_issue` sends it to SWE-AF),
and SWE-AF pauses at its plan so the owner can approve it or ask for changes.

## The keyless smoke test

```sh
./smoke.sh
```

It restarts the stack against the scripted stub (so nothing is spent, whatever keys `.env` holds) and checks,
printing `PASS` or `FAIL` for each:

- every service is healthy; AgentField, SWE-AF, PR-AF and the factory are registered; PR-AF answers a call;
- the operator link claims the organization once, a second person joins by invite, roles are enforced;
- a factory job reaches SWE-AF through AgentField with **strict run-owner proofs on**, its plan question reaches
  the owner, "ask for changes" resumes the run, and rejecting the plan ends it; AgentField's proof counters agree;
- after `docker compose restart`, the nodes register again and AgentField, the runs and the people are all still there.

The stub's answers are minimal schema-valid text, so the "work" is not real. `docker compose up -d` afterwards
puts your own keys back.

## State, backups and upgrades

State lives in five named volumes (`docker volume ls`, prefix `codeaf-org_`); `docker compose down` keeps them,
`docker compose down -v` deletes them. A backup of one volume:

```sh
docker run --rm -v codeaf-org_codeaf-data:/d -v "$PWD":/b busybox tar czf /b/codeaf-data.tgz -C /d .
```

Everything is pinned: the codeaf release (`CODEAF_RELEASE` and its two sha256 values at the top of
`codeaf.Dockerfile`, `swe-af.Dockerfile` and `pr-af.Dockerfile`), the SWE-AF and PR-AF commits (`SWEAF_REF`,
`PRAF_REF`) and the AgentField image (`AGENTFIELD_IMAGE`, tag and digest together in `agentfield.Dockerfile`).
To upgrade, change the pin, then `docker compose up -d --build`. Changing the line in the file is what makes
Docker rebuild that layer; a loose version would keep restoring the cached one.

## Secrets

No secret is baked into an image. The AgentField key, the organization token and the model keys come from `.env`
or your environment. A service that is given a key as a mounted file can use `AGENTFIELD_API_KEY_FILE`,
`OPENROUTER_API_KEY_FILE` or `GH_TOKEN_FILE` instead. Treat `.env` like a password file (`setup.sh` writes it
mode 600); it is ignored by git and by the image build.

## Railway

The same Dockerfiles run on Railway as one project: five services, a volume on each, private networking
between them, and a public domain on `codeaf` only. Nothing needs Compose there; each service builds its own
Dockerfile from this folder. (A one-click Railway template of this is the next step; until then it is built by hand,
below, and the POC that proved it ran the keyless smoke test on itself.)

Create an empty project, then one service per row. For the four built services choose **GitHub repo**, set
**Root Directory** to `deploy/org`, and set the variable `RAILWAY_DOCKERFILE_PATH` (or the service's
**Dockerfile Path**) to the Dockerfile in the row. Add a volume at the mount path in the row. Reference variables
(`${{agentfield.RAILWAY_PRIVATE_DOMAIN}}`) are Railway's way of naming another service; keep them as written.

| Service | Dockerfile | Volume | Variables |
| --- | --- | --- | --- |
| `agentfield` | `agentfield.Dockerfile` | `/data` | `AGENTFIELD_API_KEY=${{secret(64, "abcdef0123456789")}}`, `AGENTFIELD_HOME=/data`, `AGENTFIELD_CONFIG_FILE=/dev/null`, `AGENTFIELD_PORT=8080`, `PORT=8080`, `AGENTFIELD_STORAGE_MODE=local`, `AGENTFIELD_STORAGE_LOCAL_DATABASE_PATH=/data/agentfield.db`, `AGENTFIELD_STORAGE_LOCAL_KV_STORE_PATH=/data/agentfield.bolt`, `AGENTFIELD_RUN_OWNER_REQUIRE_PROOF=true`, `AGENTFIELD_TELEMETRY_ENABLED=false`. Health check path `/api/v1/health`. |
| `codeaf` | `codeaf.Dockerfile` | `/data` | `CODEAF_ROLE=server`, `PORT=8787`, `CODEAF_ORG_NAME=...`, `CODEAF_PUBLIC_DOMAIN=${{RAILWAY_PUBLIC_DOMAIN}}`, `CODEAF_TRUSTED_PROXIES=100.64.0.0/10`. Generate a public domain for this service only, target port 8787. |
| `runner` | `codeaf.Dockerfile` | `/data` | `CODEAF_ROLE=runner`, `AGENTFIELD_URL=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `ORG_MODEL=...`, `OPENROUTER_API_KEY=...`. |
| `swe-af` | `swe-af.Dockerfile` | `/workspaces` | `NODE_ID=swe-planner`, `AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `SWE_HITL_NATIVE=1`, `SWE_FENCE=off`, `SWE_PRO_ENGINE=0`, `SWE_DEFAULT_MODEL=openrouter/...`, `OPENROUTER_API_KEY=...`. |
| `pr-af` | `pr-af.Dockerfile` | `/workspaces` | `NODE_ID=pr-af`, `PORT=8007`, `AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `AGENT_CALLBACK_URL=http://${{RAILWAY_PRIVATE_DOMAIN}}:8007`, `PR_AF_MODEL=...`, `OPENROUTER_API_KEY=...`. |
| `modelstub` (keyless only) | `modelstub.Dockerfile` | none | `STUB_DELAY=2s`; then on `runner`, `swe-af` and `pr-af` set `CODEAF_BASE_URL=http://${{modelstub.RAILWAY_PRIVATE_DOMAIN}}:8890/api/v1`, `OPENROUTER_API_KEY=stub-key`, and the model variables to `stub/scripted` (SWE-AF: `openrouter/stub/scripted`); on `swe-af` also `SMOKE_FIXTURE_REPO=/workspaces/smoke-fixture`. |

Two things differ from Compose. Railway's edge is an HTTPS proxy in front of `codeaf`, so
`CODEAF_TRUSTED_PROXIES=100.64.0.0/10` (the edge's range) is required: without it every password sign-in is
refused as plain HTTP. And there is no `docker exec`, so set `RUNNER_SELFTEST=1` on `runner` to have it run the
smoke test's control and job sections on itself at start (PASS or FAIL lines in its log; `RUNNER_SELFTEST=after`
runs the after-a-restart section instead). The operator link is in the `codeaf` deploy log.

## Known limits

- The runner is a small Python host of the codeaf engine (`scripts/runner.py`), because `codeaf factory serve`
  does not itself register the factory on AgentField. When codeaf ships a headless runner this goes away.
- The organization keeps no canvases: the server needs a `canvas` program beside it, which this stack does not
  carry. It starts, says so in its log, and leaves the canvas routes off.
- The server names itself by its listen address, so without `CODEAF_PUBLIC_DOMAIN` its links say `http://[::]:8787`,
  which only the same Linux or macOS machine can use. A real deployment serves HTTPS on a name.
- GitHub sign-in (`CODEAF_GITHUB_CLIENT_ID`) and invitation mail (`CODEAF_SMTP_URL`) are optional and untested here;
  username and password is the default way in.
- One replica of each service; no high availability.
