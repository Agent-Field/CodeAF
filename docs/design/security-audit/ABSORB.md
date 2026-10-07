# Absorbing sec-af into codeaf

The security auditor was copied once from the sec-af repository and is frozen
there. This page records what was copied, what was left behind, and what was
changed on the way in, so a reader diffing `internal/secaf` against sec-af can
tell a deliberate change from drift.

## Source

- Repository: sec-af, Go module `github.com/Agent-Field/sec-af/go`
- Tag `codeaf-absorb`, commit `47d57d76ea170c44893d138862384476e3a617b8`
- Copied on 2026-10-05
- `go/internal/*` became `internal/secaf/*`; `go/docs/DESIGN.md` is
  `SEC-AF-PORT-DESIGN.md` and `go/README.md` is `SEC-AF-GO-README.md` here,
  both unchanged.

## Not copied

- `cmd/` (the HTTP node binary), `Dockerfile`, `docker-entrypoint.sh`,
  `agentfield-package.yaml`, `Makefile`, `scripts/` (the Python golden and
  schema generators).

## Deleted from the copy

- `internal/node`: the AgentField node: agent construction from the
  environment, control-plane registration and serving, git cloning into a
  workspaces directory, and the `audit` reasoner's HTTP status mapping. What
  the in-process program needs moved to `internal/secaf/audit` (below).
- `internal/audit`: an uncalled one-field stub ported only so every Python
  module had a counterpart. The name now belongs to the audit entry.
- `config/ai.go`'s provider machinery: provider selection, the external
  CLI binaries and their paths, `ProviderEnv`, the eager `XDG_DATA_HOME`
  directory, and every `SEC_AF_*` / `HARNESS_*` / `AI_MODEL` variable read.
  `AuditConfig.Provider` (unread) went with it.
- The prompt drift test that compared `prompts/files` with the Python tree:
  sec-af is frozen, so there is nothing left to drift from.
- Tests that only covered the deleted parts: node construction and env
  precedence, cloning and workspace fallbacks, provider env, the SDK router's
  `/discover` payload and router tags.

## Changed

- Imports were rewritten to `github.com/Agent-Field/codeaf/internal/secaf/...`.
- The AgentField SDK's `agent` and `harness` packages are no longer imported.
  Only `sdk/go/ai` remains, at codeaf's pinned version; no `ai` symbol needed
  an adaptation.
- `appx` and the agent loop codeaf wrote for the audit live outside the copy,
  at `internal/agentsession/appx` and `internal/agentsession`, because they are
  codeaf's and not sec-af's, and a second carried program (`/review`, being built on
  its own branch) is to run on them too. The loop names the work in its
  system prompt from `Config.Work`; sec's is `a security audit`.
- `appx` declares its own `HarnessOptions{Cwd, ProjectDir}` and
  `HarnessResult{Result, Parsed, IsError, ErrorMessage, NumTurns, DurationMS,
  CostUSD}` in place of the SDK's types. `CostUSD` stays a `*float64` because
  the cost trackers skip an unreported cost.
- `reasoners.RegisterAll` fills an in-process `Registry` (name to handler,
  plus input schema) instead of an SDK router. The 33 names, their order, and
  the input validation are the same. A refused body is a
  `*reasoners.InputError` carrying 422, where it was an SDK `ExecuteError`.
- `audit.WithLocalCalls` answers `.call` in process. It keeps the JSON round
  trip in both directions and returns a failed call as an opaque error, as the
  control-plane hop did.
- `audit.Run` resolves the repository before it builds the orchestrator, and
  passes it in through the new `orch.NewAt`. The node built the orchestrator
  against `SEC_AF_REPO_PATH` or its working directory first.
- Only an existing local directory is audited. A URL, a missing path or a
  file is refused with a 400 `*audit.Error`, and nothing is cloned.
- `phases.NodeID()` is the constant `"sec-af"`; `NODE_ID` is no longer read.
- `config.AIIntegrationConfig` keeps `AIModel` and the retry schedule.
  `DefaultAIConfig()` has the node's retry defaults (3 retries, 2s initial and
  8s maximum backoff) and no model. With no model set, the AI gate passes no
  `ai.WithModel`, so the App picks the model.
- The gates' `HarnessWrapper` (not on the live path) passes only cwd and
  project_dir; its model, max-turns and budget overrides are gone.
- `harnessx.Extract` writes its diagnostic block to `harnessx.Diagnostics`
  (default `io.Discard`) instead of stdout. The audit entry no longer prints
  `AUDIT ERROR:` to stdout; the failure note carries the same text.
- `schemas.Verdict` was renamed `schemas.ExploitVerdict`, because codeaf
  allows exactly one type named `Verdict` (the taxonomy law).
- Every `aforge`/`openaf` spelling was removed from Go sources; testdata keeps
  its recorded values.
- `invopop/jsonschema` resolves to v0.14.0, the version codeaf's module graph
  already selected (sec-af pinned v0.13.0). It is only the fallback schema
  reflector, and every golden passes on it.
