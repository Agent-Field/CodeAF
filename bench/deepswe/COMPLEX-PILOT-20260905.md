# Complex repository conversation pilot

This pilot supplements the historical five easy picks. It uses the real
`dry-python/returns` repository and the DeepSWE Validated feature request, with
159 acceptance tests and 61 regression tests. The request contains implementation
guidance; it is not presented as an original GitHub issue or a blind diagnosis.

The existing conversation adapters and tmux driver provide both native terminal
doors. The container boundary follows the existing DeepSWE rig and the chat work
reviewed in PR #634. `compare-cell.sh` adds the same inference guard used by the
conversation campaign and grades against an already controlled, frozen image ID.
It does not add a second model router or a new benchmark verdict implementation.

The pilot has one predeclared pair, Pi then Aforge, with 1,800 seconds per arm,
two CPUs and 8 GiB each. Both use `deepseek/deepseek-v4-flash-0731` and low requested
reasoning effort. All text roles are constrained by a credential-holding guard
outside the container; candidates receive a sentinel. Pi is pinned to 0.84.2 and
its custom provider advertises the 1,310,720 context capacity returned by the
OpenRouter model catalog on September 5. Harness defaults for other settings and
provider routing remain native, so this is not an isolated tool-loading ablation.
Aforge also retains a $5 session cap; Pi has the common wall limit only. That
additional cap must be reported if it binds rather than treated as an equal
monetary budget.

Candidate containers have host networking, including public network access. The
corpus, reference implementation, grader tests, host credentials and Docker
socket are not mounted. The grader has no network. Containers start from the same
runtime image, with an empty profile and the exact repository base. A `main`
branch is added at that base because the task asks for a new branch from main.
No future source refs are supplied. Network access is not proof against a
candidate fetching later public source; inspect the saved transcript when
interpreting results.

Spark is arm64 and the images are linux/amd64. Both arms and the grader use the
existing emulation workaround: `GOGC=off`, `GOMEMLIMIT=6144MiB`. These times must
not be compared with native Mac or native amd64 results. Time includes terminal
startup and the common 30-second quiet window; grading time is separate.

Completion requires a native ready screen, absence of native busy markers and
no unsettled guard admissions for the quiet window. That observation only ends
the run. Independent acceptance tests determine correctness, and a timeout stays
a timeout even if the final patch passes. Work is stopped before snapshotting.
The patch comes from the final main workspace, including committed and untracked
changes; unlanded task worktrees are retained as evidence but not selected for a
better score. Missing grader reports are infrastructure failures, never zeros.

`compare-prepare.sh` installs the pinned comparator into the controlled base and
freezes the resulting image. `compare-launch.sh` is the dated Spark pilot launcher,
not a portable campaign API: its private guard key path and evidence root are
explicit. Run it through `fleet run --cpu` from the repository root. The build uses
`make build`, and runtime startup/tool execution/billing must pass for both arms
before the scored pair starts. Existing evidence directories are never replaced.

Artifacts live outside the synchronized workspace in
`~/bench-artifacts/af653-complex-20260905`. The manifest freezes corpus hashes,
actual runtime and verifier image IDs, source binary hash, runner inputs, order
and conditions. Original and whitespace-normalized prompts, guard receipts,
terminal frames, isolated state, full workspace, patch and grade are retained.
The first Pi startup exposed a wrapper path assumption before inference; its
failed preflight remains recorded separately from the corrected retry.

One pair can expose a concrete failure. It cannot establish quality equivalence,
win rate, a frontier, or broad efficiency. The product takeover report records
results and limitations as independent grading finishes.
