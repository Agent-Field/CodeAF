# Absorbing pr-af into codeaf

The pull-request reviewer was copied once from the pr-af repository and is frozen
there. This page records what was copied, what was left behind, and what was
changed on the way in, so a reader diffing `internal/praf` against pr-af can tell
a deliberate change from drift. sec's `docs/design/security-audit/ABSORB.md` is
the worked example this one follows.

## Source

- Repository: pr-af, Go module `github.com/Agent-Field/pr-af/go`
- Tag `codeaf-absorb`, commit `b70667e`
- Copied on 2026-10-06
- `go/internal/*` became `internal/praf/*`; `go/README.md` is
  `PR-AF-GO-README.md` here, unchanged.

## Not copied

- `cmd/` (the HTTP node binary), `Dockerfile`, `docker-entrypoint.sh`,
  `agentfield-package.yaml`, `Makefile`, `scripts/`, `test/`.
- `internal/node`: the AgentField node — agent construction from the
  environment, control-plane registration, the webhook, the `review` endpoint's
  status mapping. The one piece the program needs, the table of 16 router
  reasoners, is `internal/praf/handlers.go`.
- `internal/hitl`: the approval loop that paused a review on the control plane
  for a person's go-ahead before posting. Inside codeaf no review posts from
  the pipeline; posting is its own run, on the person's yes.
- `internal/delegate`, `internal/inproc`, `carried/` and `codeaf/`: pr-af's own
  first drafts of a codeaf integration (a host mirror, its own read-only agent
  loop, a drop-in adapter). The program here uses codeaf's real
  `internal/delegate` types, and its agent sessions are sec's
  (`internal/secaf/backing`).

## Changed

- Imports were rewritten to `github.com/Agent-Field/codeaf/internal/praf/...`.
- The AgentField SDK's `agent` and `harness` packages are no longer imported;
  only `sdk/go/ai` remains, at codeaf's pinned version, and no `ai` symbol needed
  an adaptation. `internal/praf/appx` declares the review's own seam (Harness,
  AI, Note) with `HarnessOptions{Cwd, Label}` and a `HarnessResult` carrying the
  fields the review reads. Each reasoner's agent session carries a label
  (`reviewer`, `evidence`, `challenge`, …) that names its thread.
- `orch.Orchestrator.Run` runs the pipeline once. Its HITL revision loop, the
  `Pause` verb, the hax client seams and `config.HITLConfig` are gone; the
  review's hints still reach every reviewer as guidance. `orch.PostReview` and
  `orch.PostReviewEvent` post a saved review with the "own pull request" 422
  fallback the pipeline's own post used, and `Orchestrator.PRData` hands back the
  pull request a review fetched.
- No `PR_AF_*` environment is read. `config/ai.go` (provider selection, the
  external CLI binaries, `ProviderEnv`, every `PR_AF_*` model and retry
  variable) and `config/harnessbin.go` are deleted. The evidence pack stays on
  and the post-worthiness gate off, their shipped defaults. `ResolveBudgetCaps`
  takes the caller's caps or its defaults.
- `orch.ResolveRepo` takes an `orch.Access` — the folder to clone into and the
  GitHub token — instead of reading `PR_AF_WORKDIR` and `GH_TOKEN`; the five git
  timeouts are the shipped defaults; a review with nothing to clone is refused
  instead of falling back to `PR_AF_REPO_PATH` or the working directory.
- **The token never reaches the disk.** pr-af wrote `GH_TOKEN` into the clone's
  remote URL, which git keeps in `.git/config`; here it is an Authorization
  header in one git process's environment (`GIT_CONFIG_COUNT`).
- `github.NewClient` takes the token it is handed and reads no environment.
  GitHub App sign-in (`GITHUB_APP_ID`, its private key, `golang-jwt`) is not
  carried. `OpenPullRequest` is new: the open pull request from a branch, for a
  bare `/pr`.
- `LFS` smudging is always skipped (`GIT_LFS_SKIP_SMUDGE=1`): the review reads
  code.
- The pipeline's progress prints go to stderr; stdout is the program's record
  stream.
