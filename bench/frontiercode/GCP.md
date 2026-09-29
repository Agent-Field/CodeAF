# The FrontierCode rig on GCP

*Written 2026-09-28. This turns `bench/frontiercode/` from a local Docker rig
into a GCP-native one. It models the campaign machinery in
`~/Code/swe-pro-go-deepswe-minimal` — the operational disciplines, not that
repository's harness, corpus, verifier, variable names, key names or pinned
build. Every number below comes from the campaign manifest; nothing here is a
default.*

The rig's account of itself, its task format, its rubric and its controls is
still `README.md`. This page is only about the move to GCP: the manifest that
is the single source of truth, how many hosts and how big, the scripts, what was
adapted from the reference and what was copied, and what a campaign costs.

## 1. The campaign manifest is the single source of truth

One JSON file per campaign, selected with `FC_MANIFEST` (a name under
`bench/frontiercode/` or a path). The sample a clean checkout carries is
`manifest-frontiercode-pilot.json`. An unset, unreadable or unparseable manifest
refuses; a missing required field refuses. There is no fallback value anywhere:
`gcp-lib.sh`'s `fc_get` exits when a field is null or empty, so a manifest that
cannot answer the question stops the run instead of guessing.

| field | what it pins |
| --- | --- |
| `schema` | manifest shape version |
| `campaign_name` | human name, also the GCP `campaign=` label |
| `preregistration` | path, repository-relative, to the registered page; launch refuses when it is absent |
| `label_prefix` | every iteration label begins here, so labels are globally unique per campaign |
| `arm` | the arm under test; also the GCP `lane=` label |
| `seed_ids` | the swept trials: 5 is the official protocol's default; a one-element list runs a single trial (a canary). One is chosen per iteration label, recorded per run |
| `reasoning_efforts` | the swept reasoning levels: launch enumerates every one, and the report states the per-level metric and the best-performing level. One element pins the level |
| `hypothesis` | the claim, copied into every iteration's `meta.txt` before launch |
| `model` | served model, provider-qualified without the provider prefix (`deepseek/deepseek-v4.1-flash`) |
| `provider` | the provider the model is served through (`openrouter`) |
| `list_price_per_mtok` | `input` / `cache_read` / `output`; a non-positive price is a refusal |
| `judge_model`, `judge_prompt_version` | the LLM judge pinned per campaign; the grader records both with its token usage |
| `codeaf_commit` | the source commit the pinned binary was built from |
| `codeaf_sha256` | sha256 of that linux/amd64 `bin/codeaf`; staging re-hashes on the host and refuses a mismatch |
| `codeaf_local_binary` | path, repository-relative, to the pinned binary the Mac stages |
| `rig_commit` | the pinned rig commit. `HEAD` resolves to the committed tip at stage time (a commit cannot name itself); a literal is honoured and staging refuses unless HEAD equals it |
| `corpus` | the task corpus directory, repository-relative |
| `corpus_sha256` | digest over every corpus file and its path, computed by `gcp-lib.sh` |
| `host_shards` | how many hosts, named `<instance_prefix>-s1..sN` |
| `wave_capacity` | most concurrent containers in one wave, one wave per label |
| `per_container` | `cpus` and `memory_gb`; the host memory gate multiplies the latter by the wave capacity |
| `shard_files`, `shard_sha256` | shard number -> task list path, and its exact bytes |
| `max_cost_usd`, `max_hours`, `hard_timeout_seconds` | the cost and wall caps the launcher exports into every run |
| `reasoning_effort` | the variant recorded per run |
| `gcp` | `project`, `zone`, `image`, `machine_type`, `boot_disk_gb`, `instance_prefix`, `max_run_duration` |
| `key` | `store` (`keychain`, `file` or `env`) and `item` (the secret-store name) |
| `recovery` | **declares** contract exceptions (`note`, `profiles`, `label_prefixes`) rather than hard-coding one in a script; empty means none |

The frozen-population check (`fc_frozen_check`) recomputes the corpus digest and
every shard hash and compares them to the manifest before a host is contacted or
a wave launched. A drift is a refusal, not a warning.

How this rig conforms to the official FrontierCode specification — and which
requirements cannot be closed at all — is tracked row by row in
[README.md §13 "Conformance with FrontierCode"](README.md#13-conformance-with-frontiercode).

## 2. Hosts: count and sizing

- **Count**: one host per shard, `<instance_prefix>-s1..sN`, where `N` is
  `host_shards`. A campaign never lists or touches an instance outside its own
  prefix.
- **Sizing**: a task container is about 2 CPU / 8 GiB, so a host must carry
  `wave_capacity * per_container.memory_gb` of memory plus the OS and the image
  cache. The launcher refuses a host whose memory cannot cover one wave. The
  sample pins one fixture, one shard, wave capacity 2, and `e2-standard-8`
  (8 vCPU / 32 GiB) — 16 GiB of containers on a 32 GiB host, with room for the
  image warm cache.
- **Boot disk**: sized to hold every task image plus the Go toolchain; the
  sample uses 120 GB `pd-balanced`.
- **Self-stop**: every host is created with `--max-run-duration` (the manifest's
  `max_run_duration`) and `--instance-termination-action=STOP`, so an abandoned
  campaign stops rather than bills forever.
- **Image**: the GCP image must ship `docker`, `python3`, `git`, `jq`, `curl`
  and `iproute2`; the script installs Go if it is absent. The sample names a
  base the owner publishes (`codeaf-frontiercode-base-20260928`) — no create
  happens in this pass, so it is a declaration rather than a claim.

## 3. The scripts

All five source `gcp-lib.sh`. Every one has a path that runs its gates and
touches nothing: `--plan` (and `gcp-stage.sh --check`, `launch.sh
--check-local` / `--check`, `fetch-results.sh --check`).

| script | modes | what it does |
| --- | --- | --- |
| `gcp-create.sh` | `--plan`, `--create` | `--plan` prints the full campaign plan and needs no credentials; `--create` refuses an existing name, labels hosts `campaign=`/`lane=`, and sets `max-run-duration` + `STOP` |
| `gcp-stage.sh` | `--plan`, `--check <instance> <shard>`, `--stage` | credential-free staging: a git bundle of committed HEAD keyed by a ref, the pinned binary re-hashed on the host, the rig commit checked out, the corpus pinned, the egress proxy bound and its address verified, then one background image warm loop guarded by a child-written PID file |
| `gcp-key.sh` | `--plan`, `--check`, `--install <instance>`, `--usage [instance]`, `--shred <instance>` | `--plan`/`--check` read nothing and transfer nothing; `--install` pushes the campaign key over ssh and prints only its length, a sha256 prefix and the provider usage counter; `--shred` before teardown |
| `launch.sh` | `--check-local`, `--check <shard>`, `--execute <shard>`, `--replace <shard> <task>` | the gates and the four modes; waves of at most `wave_capacity`, one label each; `--replace` is the only re-run path |
| `fetch-results.sh` | `--plan`, `--check`, `--fetch <instance> [--keep-host]`, `--teardown <instance>` | fetches every DONE-carrying run whole, verifies its full artifact set, exits non-zero and deletes nothing when anything is missing; then reports, shreds the key and deletes the host and its boot disk |

**Staging transfers no credential.** It sends the bundle, the binary, a small
config file of hashes and the remote script; the key is never on that path. The
remote half runs each step on its own line so a failure names itself — the
reference learned this when an `&&`-chain reported success while leaving a host
on the wrong verifiers.

**The egress proxy is bound and its address is verified.** The host builds
`proxy/egress-proxy.go`, binds it on the Docker bridge (`172.17.0.1:3128` by
default, discovered from the bridge), and proves it listens on that address with
`ss` and a request through that address — not a localhost health check that a
process bound to the wrong interface would pass. The agent path itself still
runs the rig's per-run proxy inside `run.sh` on each run's internal network,
with `grade/scanner.py` reading the log afterwards.

**The image warm loop is guarded by a PID file the child writes.** It is
`setsid nohup` into a file that begins `echo $$ > pidfile; exec ...`, so
liveness is a fact about the process. A `pgrep -f` guard cannot work here: the
whole remote script arrives as one ssh command whose text contains the loop's
own invocation, so the pattern matches the shell evaluating it and a freshly
staged host reports "already running" with nothing running at all.

## 4. Adapted from the reference, or copied

The reference repository's campaign machinery was read before anything was
written. The rule was its operational disciplines, not its harness.

| mechanism | choice | why |
| --- | --- | --- |
| manifest per campaign as the source of truth | **copied** | This is the discipline worth keeping: one frozen file, hashes in it, a refusal when it cannot answer. Field names follow our shape, never the reference's. |
| `--plan` before `--create`; refuse an existing instance | **copied** | Same safety property; the reference's plan also lists the project, ours lists only its own prefix because a campaign must never touch or name another. |
| `max_run_duration` + `instance-termination-action=STOP` | **copied** | Directly. An abandoned campaign stops. |
| git bundle, dirty-rig refusal, pinned binary re-hashed | **copied** | The bundle is committed HEAD only. The reference picked a ref for its codeaf commit; we key the rig bundle by a branch ref for the same reason — a bundle names refs, not bare revisions. |
| Go install if absent | **copied** | The base image does not ship it. |
| one image-pull loop guarded by a child-written PID file | **adapted** | The reference pulls frozen task images; this rig builds its own from the committed Dockerfiles, so the loop warms (builds) the shard's images. The PID-file discipline is copied unchanged, for the reason it exists. |
| control plane bound on the Docker bridge, address verified | **adapted** | The reference rebinds `mock-cp`; this rig has no mock control plane. The analogous host service is the campaign-level egress proxy, so that is what is bound on the bridge and verified. The per-run agent exit remains `run.sh`'s own proxy container. |
| allowlist egress proxy | **dropped, inverted** | This rig's policy is the opposite: open and logged. `proxy/egress-proxy.go` was already derived from that proxy with the allowlist removed; GCP changes nothing about it. |
| key out of band; length + digest + usage counter | **copied in shape, adapted in names** | The key lives in the local secret store the manifest names and is pushed out of band; the reference's Keychain item name is not used. A key may be reused across campaigns: this campaign's provider-side spend is the delta of the usage counter between the baseline recorded at install and the final read at teardown, valid on the assumption that nothing else used the key in that window. |
| `--check-local` / `--check` / `--execute` / `--replace` | **copied** | Four modes, the same gates. `--replace` is the single preregistered re-run path with the original attempt preserved. |
| `fetch-results.sh` with a completeness proof before teardown | **copied** | Whole directories, DONE-carrying runs only, exit non-zero on a missing artifact. The reference carries a richer pre-agent contract-failure exception; ours requires the five run artifacts and the grade. |
| DeepSWE's verifier, corpus, `deep_swe_revision`, `official` block, `DELTA_*` variable names, `codeaf_args` | **dropped** | Not ours. The grader is `bench/frontiercode/grade/`; the corpus is `bench/frontiercode/tasks`; the judge is pinned in our manifest. |
| `adopt_image_tree`, `codeaf_env` knobs, `band_counts` | **dropped** | They exist because of that harness's workspace contract, routing and difficulty bands. Our single fixture needs none of them; a future campaign adds a field rather than a borrowed one. |
| `recovery_profiles` / `recovery_label_prefixes` | **adapted** | Kept as a `recovery` block that *declares* contract exceptions. The brief demands that an exception be declared in the manifest rather than hard-coded in a script; a campaign that expects none carries empty lists. |

The controls survive the move untouched: `gold.sh`, `negative.sh` and
`seal.sh` grade a patch through the same `grade.sh` and `grade/` that run on the
host, and a missing grade is recorded as `rig`, never a silent 0. The rubric's
six criterion kinds, the `--network none` verifier container and the host-side
judge with its token usage are all unchanged.

## 5. Cost model

- **Hosts bill by the hour while they exist.** `e2-standard-8` is on the order
  of \$0.27/hour in `us-central1` before the boot disk; the disk is billed
  separately while the instance exists (it is deleted with the instance). A
  six-hour cap on one host is therefore a few dollars of compute, bounded by the
  self-stop.
- **Model spend is metered twice per run, as before**: the harness's own ledger
  and the credential guard's meter read from the provider's usage rows. The
  guard's reading is the one the table carries. On this fixture the local rig
  measured roughly \$0.04–0.12 per rollout at open-model prices; the judge adds
  about \$0.006 per prompt criterion.
- **The judge runs from the host**, so grading costs CPU on the host plus one
  judge call per prompt criterion; gold and negative cost CPU and judge calls
  only.
- **A campaign is not launched without a budget figure.** The manifest's
  `max_cost_usd` is per run; the campaign plan prints the caps, and instance
  creation waits for the owner's explicit go-ahead. No instance was created and
  no model money was spent in this pass.

## 6. What this pass proves, and the deviations

This pass ships the scripts and a sample manifest and proves the
touch-nothing path: `gcp-create.sh --plan` prints the complete campaign plan
from a clean checkout without credentials, and every script passes `bash -n`.
The end-to-end proof the brief allows to be plan-only is plan-only: **no
instance was created, started or deleted, and no model money was spent.**
`--execute` and `--replace` run on a staged host and were not exercised here;
their gates are the ones the brief names, and `launch.sh --check-local` runs
against the sample manifest today.

Two deviations, stated rather than hidden:

1. **`rig_commit` may be `HEAD`.** A commit cannot contain its own hash, so a
   literal pin would have to be written after the fact. `HEAD` resolves the pin
   at stage time and records it; a later campaign may set a literal and staging
   will refuse unless HEAD equals it.
2. **The host-level egress proxy is the campaign's logged exit, not the agent's
   only exit.** The agent's egress on every run is `run.sh`'s per-run proxy
   container on the run's internal network, which is where the scanner's
   evidence comes from. Binding a campaign-level proxy on the bridge satisfies
   the address-verification discipline and gives the harness one logged exit for
   its own fetches; it does not replace the per-run proxy the brief requires on
   every agent path.

The naming law holds across every line: the product is `codeaf`, lowercase, and
no retired spelling appears outside a `legacy-name` marker.
