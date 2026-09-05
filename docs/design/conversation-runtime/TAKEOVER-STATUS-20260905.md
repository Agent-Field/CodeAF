# Conversation execution takeover — 2026-09-05

The work continues from the “Aforge clean” Codex thread, in isolated worktree
`/private/tmp/af-runtime-next`, on `codex/conversation-execution`. Draft PR #653
targets `dev`. The shared development checkout is not this worktree.

## Product direction

One owner remains responsible for the requested outcome. Direct tools handle
ordinary actions; short forks handle independent slices; durable tasks own work
that needs its own lifetime. Loading a specialist tool is a local registry
operation, not another agent, a new task, or an inference-based routing decision.

Use one discovery level. Keep everyday file, shell, job, task, manual and web
tools directly available. Chat may defer media, settings and saved-procedure
schemas until requested. Retain original tool names and the existing execution,
approval and accounting paths. Append loaded tools without reordering the active
list. Task workers and reduced fork/auditor belts keep their existing contracts.

The tradeoff is explicit: less repeated schema data on ordinary requests, one
additional model request on first use of a specialist group, and a changed
provider prefix when that group loads. Byte savings alone do not establish
lower latency, lower bills, or equal success.

## Implemented before tool discovery

- Replacement windows receive pending questions even while the old window is
  still attached. Initial welcome and replay deliver the question once, from one
  coherent snapshot. Reconnect still delivers questions missed during a gap.
- A completion claim can decline handoff without a second reader explicitly
  agreeing. A sketch with independent work remaining still vetoes the claim.
  Every accepted claim is charged once per request, including corroborated ones;
  ten more rounds of work bring the ceiling back. A new user direction changes
  the request. Existing running commands retain their owner and completion wake.
- Canceling before or during handoff preparation prevents task admission; a
  canceled reader does not fall through to a worker receiving the original ask.
  Regression tests failed before these changes and passed afterward.

## Prior live evidence retained

At source `70888a56b`, the two-repeat revision diagnostic recorded one Aforge
pass and one timeout; Pi passed both. The timed-out Aforge attempt still has an
unsettled charge after a read-only reconciliation attempt. Its correct files do
not override its missed deadline.

In the successful diagnostic pair, Aforge made 18 admitted calls and sent
910,727 request bytes; Pi made six calls and sent 56,038 bytes. Aforge's main
requests carried 34 tool definitions. These are one pair's observations, not
population estimates or an attribution of all overhead to tool schemas.

## Still separate work

- Automatic handoff can still trigger after two distinct files or five writes.
  Completion changes reduce one bad outcome of that rule; they do not remove it.
- A stale sketch that names independent parts can still veto a current
  completion claim. Consolidating ownership decisions remains unfinished.
- Wider real-repository, chat and interruption comparisons remain necessary;
  a tiny fixture battery cannot establish a Pareto frontier.

## Integrated specialist discovery

Source `9f0ce1619` includes the reviewed discovery implementation and the other
fixes above. `load_capability` lists actual available tool names in three possible
groups; it adds no classifier or model-routing call. Errors remain tool errors,
loading preserves existing permissions, and normal tools execute under their
original names. Reopen restores load calls still present in the saved transcript;
compacted load records may need loading again. There is no separate persistence
store and no nested execution wrapper.

Serialized measurements from complete JSON tool blocks:

| Configuration | Before | After | Saving |
| --- | ---: | ---: | ---: |
| Standard fixture, prompt + tools | 47,606 bytes | 44,347 bytes | 3,259 bytes (6.8%) |
| Fully enabled chat, tools only | 40,595 bytes | 26,740 bytes | 13,855 bytes (34.1%) |

The fully enabled loader itself is 708 bytes. The standard fixture includes the
heavier prompt wording for discovery; the prefix budget stays 48,000 bytes. These
are encoded bytes, not provider-token or billing measurements. The larger saving
requires the richer configured tool set and is not the default-chat saving.

## Offline validation record

The full `make check` run at `9f0ce1619` completed 85 passing packages, including
`cmd/aforge`, `internal/remote`, `internal/enginehost` and the full `internal/tui3`
suite (464.828 seconds). Four tests failed across three packages: the worker
vocabulary law, the chat-manual vocabulary law, and two media-schema fixtures
that inspected the initial tool list without loading media. Those failures are
retained in `takeover-final-check.log`.

Commit `eccef810e` corrects the wording and makes both schema fixtures load the
media group before inspecting the real active schemas. All four focused
regressions pass. The three affected packages then passed in full through
`make check PKGS='./internal/exec ./internal/manual ./internal/session'`: execution
106.290 seconds, manual 1.514 seconds, and session 183.534 seconds. Vet, formatting,
packed-manual tests, release build and size ratchet also passed. The binary is
50,552,226 bytes against a 54,600,000-byte cap. The repository's one existing
known-red exclusion was retained; no new exclusions were added. This is a full
suite with corrected affected-package reruns, not a claim that the initial
full-suite invocation exited successfully.

The first remote CI run at `b255c23c4` passed the light gate and seven of eight
touched packages, including the full terminal suite (466.187 seconds). The
session package exposed one more fixture dependency: the load-and-call acceptance
test expected a media group without providing media, relying on a locally
installed FFmpeg. Running that test with `/usr/bin:/bin` as PATH reproduced the
failure on the Mac. The fixture now provides scripted media explicitly; the
separate absent-capability tests retain their checks of unavailable groups. This
is a test-environment correction, with no runtime change to the frozen complex
comparison build. The failed CI log is retained and the corrected gates must pass.

## Live tool discovery smoke

On `eccef810e`, a fresh hosted test profile received: "What is my daily budget
set to? Read the current setting and report its value. Do not change any setting
or file." The model loaded `settings`, called the original `settings` tool with
`search: "daily budget"`, and reported the returned $500 value in the same turn.
The workspace remained empty. This is the isolated fixture's setting, not an
inspection of the user's personal configuration.

The answer used three model requests and took 18.940 seconds in the session's
usage record. Tool counts were 25 → 27 → 27. Two further requests handled
background work; one lost its usage block when the host closed. A read-only
generation lookup settled that missing bill without repeating inference. All
five admitted requests are now priced: **$0.00186847322 total**, including
**$0.00180473322** for the answer's three requests. Every request used the exact
DeepSeek pin. This proves the loading/continuation path; it is not a comparison
against another harness.

## Current revision regression

The frozen `takeover-revision-v1` campaign at `eccef810e` completed all four
planned cells. Both Aforge and Pi passed both repetitions, including adopting a
mid-work requirement change while preserving the already-running command. Every
assertion passed and every cell has attributable upstream billing.

| Arm | Passes | Mean wall time | Mean billed cost |
| --- | ---: | ---: | ---: |
| aforge | 2/2 | 85.504 s | $0.002696575 |
| pi | 2/2 | 90.343 s | $0.000496991 |

This is regression evidence from a small fixture, not a quality ranking. Provider
endpoints varied between calls; it is a native-harness comparison, not an isolated
tool-discovery ablation. The planned synthetic four-defect pipeline campaign was
not started: the next evaluation is substantial work in an unrelated repository.

## Complex repository evaluation in progress

The first paired pilot is the DeepSWE `dry-python/returns` Validated feature:
container interfaces, applicative error accumulation, converters, decorators,
pointfree dispatch and Hypothesis integration. It has 159 acceptance tests and
61 regression tests. It is a benchmark feature request with repository provenance,
not a published GitHub issue. Its prompt includes implementation guidance, so this
measures implementation and integration rather than unaided defect discovery.

Base and reference controls scored respectively 0/159 and 159/159 acceptance
tests, with 61/61 regression tests in both. The cached independent grader image
ID still matches the negative-control record. Candidate and comparator will use
the same frozen runtime on Spark: two CPUs, 8 GiB, linux/amd64 emulation on arm64,
and the documented Go emulation workaround. Each gets 1,800 seconds, the exact
DeepSeek pin, low requested reasoning effort, an isolated profile, and its own
credential-holding guard outside the container. Both use the existing tmux
conversation driver. Hidden tests and the reference implementation never enter
the candidate container. No quality result is claimed before independent grading.
