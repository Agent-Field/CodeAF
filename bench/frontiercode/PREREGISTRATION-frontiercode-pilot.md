# FrontierCode GCP pilot — preregistration

*Registered before any model call. The manifest
`bench/frontiercode/manifest-frontiercode-pilot.json` is the campaign; this
page states what the campaign is for, before its numbers are visible.*

## Question

Does `codeaf senior-dev`, run on a GCP host through this rig's own task images
and its open-and-logged egress, clear every blocker on the
`jsonschema-log-warning` fixture and pass the sealed/negative controls — the
same result the local Docker rig produced — when the grading path, the egress
log and the controls all live on the host rather than on the developer's Mac?

This is a mechanism and delivery pilot, not a rate. One fixture at one seed is
a working slice; no per-task claim and no comparison to Cognition's private
FrontierCode leaderboard is made, and none of these numbers may be presented
as one.

## Fixed before launch

- **Arm**: `codeaf-senior-dev`. **Seed**: one (`s1`). **Attempts per task**: one.
- **Model**: `deepseek/deepseek-v4.1-flash` served through OpenRouter, priced at
  the endpoint rate the manifest records.
- **Harness**: the pinned `codeaf` commit and the sha256 of the linux/amd64
  `bin/codeaf` the manifest pins. Judge: `openrouter/anthropic/claude-sonnet-4.5`,
  prompt version `fc-judge-1`, recorded in every grade with its token usage.
- **Population**: every task under `bench/frontiercode/tasks`, frozen by the
  manifest's `corpus_sha256` and the per-shard hashes. The single fixture is one
  shard, one wave at capacity 2.
- **Resources**: 2 CPU / 8 GiB per container; host sized from the wave capacity
  and refused when its memory cannot cover a wave.
- **Caps**: cost and hours from the manifest, a hard timeout enforced on the
  agent, and a `max_run_duration` on the host that ends in STOP.

## Egress

Egress is open and logged, never an allowlist: the agent container exits through
`proxy/egress-proxy.go` on its own internal network, and `grade/scanner.py`
reads the proxy log and the harness transcript afterwards. A run that visited
the task's upstream repository or its patch shapes scores 0 and counts toward
flag rate. The model key is held by the credential guard, one key per campaign,
so its provider usage counter isolates this campaign's spend; that counter is
recorded at key install and beside the grade.

## Controls and failure law

The controls run against the host path as well as locally: gold scores 1.00,
the first-line-only negative scores 0.0 with both blockers failed, and the
sealed fixture is flagged and scores 0. A missing grade is recorded as a **rig**
failure, never a silent 0. No result is quoted until the controls are green on
the same host path that produced it.

## Re-runs

`launch.sh --replace <shard> <task>` is the single preregistered re-run path. It
applies the same gates under a different label, and the original attempt is
preserved in place. Infrastructure-invalid attempts may be replaced only after
the cause and the replacement designation are recorded, before the replacement's
score is looked at. No command weakening and no silent timeout extension.

## Retrieval and teardown

Every completed run comes home whole — trajectory, patch, grade, scan and both
cost readings — and `fetch-results.sh` exits non-zero and leaves the host
standing when any artifact is missing. Only after the local copy is proven
complete is the key shredded and the host and its boot disk deleted. Nothing
bills after results are in.
