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

## Correctness-first task interaction pass

The user's priority is now dependable developer workflows before efficiency;
small-task costs remain diagnostics. The default local window supplied a
`TaskRoom` reader over the engine connection but no remote host label. The UI
mistakenly required that label before using the reader, so clicking a live task
reported that the session had no task rooms. The regression failed on the old
condition and passes when the UI selects the available capability directly.

A second regression reproduced accepted corrections disappearing on the next
journal refresh. Refresh now retains unchanged entries, preserves open/full state
when content grows, and keeps accepted corrections until their journal entries
arrive. Repeated identical corrections remain distinct. The same loading wording
works on a local engine and on a remote machine.

A raw-input test sends SGR mouse clicks, typed text, Enter and Escape through
Bubble Tea's actual input decoder/program loop, with a real loopback engine
client. It verifies opening the intended task, reading its journal, delivering a
correction once to task 7, and returning to the main conversation. The fixture
provides the roster and scripted engine; it does not establish model quality or
claim to control the user's existing terminal window. The focused interaction
and hosted-room tests passed under the race detector.

The media-fixture correction's full session check passed (191.714 seconds).
CI at `3768a93ce` then exposed a separate ordering assumption in the failed-handoff
fixture: its background task was not guaranteed to fail before the closing
response. The fixture now explicitly waits for graph completion. Thirty race
runs passed. That CI run's full terminal package also passed (467.450 seconds).
The new task interaction changes require a fresh package check.

### Task interaction validation outcome

The full terminal package passes with these changes (466.970 seconds), and the
manual package passes. The session package's run failed only its optional live
compaction smoke: the returned marker was `ROUND-04`, instead of the complete
`ROUND-04-UNIQUE-TAIL`. That test allowed only 64 output tokens, shared with
reasoning. Its allowance is now 512 and failure output includes the finish
reason; three fresh pinned-model runs pass (5.078 seconds total). No compaction
runtime behavior was changed to accommodate that model response. The final
offline session gate is run without provider credentials; live checks are
reported separately rather than making ordinary package checks depend on a
provider response.

### Complex pilot outcome and diagnostic grade

Both original terminal runs ended at their ready screens: Pi in 1,232 seconds,
Aforge in 1,282 seconds. Those times include startup and the quiet window; they
are not times to equivalent completed work. Both original grades are zero.

Pi produced an implementation. Its added test package collided with the hidden
suite's module names, so the original acceptance collection never ran. Aforge's
only task failed with an upstream HTTP 404, and its final main-workspace patch
is empty. No unlanded branch was substituted to improve that score.

A supplementary diagnostic excludes **all candidate `tests/` changes** from each
saved patch, preserves every other change and runs the same frozen verifier
without network or additional inference. Results:

| Saved implementation | Acceptance | Regression |
| --- | ---: | ---: |
| Pi, with candidate tests excluded | 159/159 | 61/61 |
| Aforge, with candidate tests excluded | 0/159 | 61/61 |

This diagnostic does not replace the original failed run. It establishes that
Pi's implementation passes the assertions once the collection collision is
removed; it does not repair Aforge's missing implementation. The original
patches, grades and logs remain intact. The diagnostic script and its patch
hashes are retained under `af653-complex-20260905/diagnostic-test-isolation` on
Spark; the invocation script is at that artifact root.

The Aforge task's unreadable refusal begins with the gzip signature. The
benchmark proxy preserved compressed error bytes but dropped `Content-Encoding`
and retry headers, unlike its success path. An offline compressed-404 regression
fails before the proxy fix and passes after it. Error headers now retain the
same end-to-end contract as successful responses. This does **not** explain away
the upstream 404, prove it is fixed, or turn the failed task into a success.
The corrected proxy was not used retroactively in either scored run.

Every admitted inference used `deepseek/deepseek-v4-flash-0731`: Pi made 76 calls;
Aforge made 120. Read-only reconciliation prices all Pi calls at $0.048953800644.
Aforge has 116 priced calls out of 120, so its total remains **unknown**. No
cost ratio is reported. Emulation also drove Aforge's UI and engine close to the
8 GiB container cap; this run used the declared `GOGC=off` workaround and does
not establish native runtime memory demand. One pair with these failures cannot
establish quality equivalence, a performance ranking or a frontier.

The temporary credential file used by this pilot's guard was removed after both
runs and reconciliation ended. Further complex-work validation should first
resolve the provider refusal with readable diagnostics, then repeat under a
predeclared test-isolation policy. The practical priority remains opening,
steering and recovering real work, ahead of schema or small-task cost tuning.

Final gate for the task UI wave: the full terminal suite passed above; the
credential-free `make check` for session/manual passes (session 181.771 seconds),
including vet, formatting, packed manual, build and binary-size ratchet. The
binary is 50,552,770 bytes, below 54,600,000. All 156 conversation benchmark
Python checks pass (44.738 seconds). A new complex Aforge trial will retain the
original runtime/model/limits and change the proxy's error-header behavior;
its result will be recorded separately from the original paired pilot.

### Read failures and the recursive-core review

A further task-page regression reproduced temporary read failures presented as
missing history. Finished tasks stopped polling after that failed first read.
Opening and refreshing now share one captured reader, show a steady retry message
on failure and retain any transcript already displayed. The focused running and
finished recovery cases pass with the local task/wire tests under the race
detector. A fresh terminal package check follows this change.

The user's proposed “Pi spawning Pis” model is already the implementation's core:
workers and forks create the same Agent. `RECURSIVE-CORE-20260905.md` inventories
the surrounding duplicate decisions and defines the smaller target. It is a
proposal for removing policy layers, not a claim that the two-file/five-write
promotion rule or the extra routing readers have already been deleted.

The task-read recovery gate passes: `make check` for terminal/manual, with the
full terminal suite at 467.541 seconds, manual at 1.541 seconds, packed manual,
vet, formatting, build and size all passing. The binary is 50,552,722 bytes.
Remote CI for the preceding `120806c17` task-click/refresh wave also passes both
halves (run 33984222248); the recovery follow-up is a later commit.

An important limit on the architecture inference: the original complex run's
40-round ceiling recorded `dropped:work-already-out`. The existing ownership
check prevented another handoff there. We do not attribute that run's failure
to a forced handoff that never happened. Its recorded request traffic was
71,984,610 bytes across 120 calls for Aforge and 17,282,734 across 76 for Pi;
19 Aforge requests carried no tools. These are workload observations with failed
outcomes, not proof that every helper call is unnecessary or a cost comparison.


### Corrected-proxy complex retry: deadline without delivery

The diagnostic retry completed at the 1,800-second limit (`door.json` reports
1,801 seconds and `ended: cap`). Its runtime binary hash is unchanged from the
original pilot, `593d7400b068b3182cbad72ae73842b8b06615ce8f94edd87d32be6db1b2a7aa`;
only the proxy correction was introduced. Both the original verifier and the
predeclared test-isolation diagnostic returned **0/159 acceptance, 61/61
regression**. This is a failed delivery, not an efficiency result.

The final main-workspace patch contains a QEMU ruff core dump, with no feature
implementation. The sole child, task 2, has implementation and test edits in its
isolated worktree, but they were not integrated before the deadline. Those
unlanded files were not substituted into either score and have not received an
independent correctness grade. The final task record is paused for resumption
after the harness stopped the engine; this does not constitute completion.

The main conversation investigated for 10 minutes 26 seconds and 58 tool calls
before the runtime moved the work into that task. At 18:51:02 UTC the child
started `python -m pytest tests/ -q -p no:cacheprovider`, piped through `tail -30`,
with a 600-second timeout. It became background job 4 after 30 seconds. Its
result had not reached the child journal when the harness stopped at about
19:00:48 UTC. Toolchain preparation also encountered missing test plugins and a
QEMU ruff crash. These observations identify time spent and an undelivered
result; they do not isolate policy overhead from model behavior, environment
setup or emulated execution speed.

All 126 admitted calls used the exact allowed model; 126 have settlement
records, but only 122 have prices after read-only reconciliation. Total cost
therefore remains **unknown**. Recorded request traffic is 56,331,950 bytes. The
retry is not a new paired comparison, and it cannot establish a cost ratio.
Its evidence is retained on Spark under
`/home/santosh/bench-artifacts/af653-complex-guard-retry-20260905`, including the
manifest, native screen, complete saved state, both grades and reconciled
receipts. The owned temporary guard credential was removed at job completion.

The next architectural change should shorten the path from assignment to a
stable owner, preserve that owner's context, and make test progress and result
integration dependable. Additional nested wrappers for ordinary tools would
not address the failure observed here. The UI fixes in this wave have passed
their local gates, but this complex run prevents claiming that substantial
end-to-end developer work is reliable yet.


CI run 33985211705 passed the full terminal suite (468.315 seconds) and session
suite (162.085 seconds), but failed a reconnect fixture that required the new
window's arrival snapshot to report zero other attachments. Client socket close
does not acknowledge server EOF processing: the replacement may arrive before
the old connection detaches, as the server's detach contract explicitly allows.
The fixture now retains conversation identity, completed work and eventual lane
cleanup checks without asserting that unsupported arrival order. Separate
simultaneously attached-window tests still check the attachment count.

The old reconnect assertion reproduced 20 failures in 300 local runs. With the
first window explicitly subscribed, all 300 revised runs pass under the race
detector (35.177 seconds). The full enginehost package passes (13.865 seconds),
with vet, formatting, packed manual, build and size gates. An accidentally
started all-package gate was stopped and is not counted as validation; the
intended enginehost gate completed successfully.


## Production-readiness continuation

The preceding reconnect fix passed both CI halves (run 33986235213). The next
pass keeps correctness ahead of cost and does not change model routing, prices,
automatic handoff thresholds or task allowances.

The sole unlanded child from the corrected-proxy retry received an offline
implementation diagnostic. Using a separate Git index, it preserves the saved
worktree and excludes all candidate tests changes, then applies the resulting
patch in the original frozen verifier. It passes **159/159 acceptance and 61/61
regression assertions**. This does not replace the failed delivered-workspace
score. It narrows the observed failure to timely completion and delivery; the
saved implementation itself passes these assertions. Evidence is under
`af653-native-quality-20260905/child-diagnostic` on Spark.

A native ARM64 correctness environment now uses the same repository base and
feature request, with the public Poetry-lock development dependencies and
check-laws/compatible-mypy extras installed before the run. The native verifier
passes its controls: base 0/159 acceptance and 61/61 regression; reference 159/159
and 61/61. It retains both the raw delivered-workspace grade and a supplementary
grade excluding candidate tests, with a predeclared 45-minute trial allowance.
Different architecture, environment preparation and allowance mean this is not
a paired performance comparison with the earlier emulated runs.

Preparing the native binary exposed a cross-build defect: `make build` inherited
GOOS/GOARCH into the manual generator, attempting to execute a Linux generator
on macOS. Generation now clears those target variables, like the existing
furrow fetcher. The final binary still uses the requested target. The old build
failed with `exec format error`; the revised Linux ARM64 build completed and
`file` confirms a statically linked AArch64 executable.

Separately, a worker's deadline was only checked after completed tool events.
A silent model request, command or promoted job could therefore defer the
checkpoint until another event arrived. The runner now arms one timer for its
current deadline, rearms on an allowed extension and disarms when stopped or
finished. The existing progress decision, cancellation and delegated-parts pause
rules remain. A deterministic silent-worker test fails before the change. A
real-shell test confirms that an owed command can remain parked through one
renewal, with no extra worker request, then stop when renewal is refused.

The first full session/manual check found only the structural complexity gate:
the combined timer/event drain exceeded its function ceiling. Event observation
and accounting are now a separate method from waiting on events and the clock.
No complexity allowance was raised and no test was skipped. Fresh final checks
follow this refactoring.


Final deadline/build gate passes: full session 181.020 seconds, manual 1.515
seconds, packed manual 1.660 seconds, vet, formatting, build and binary size.
The focused deadline/renewal/delegated-pause/complexity cases also pass under the
race detector (three repetitions, 7.582 seconds). The binary remains under its
54,600,000-byte ceiling. No new skips or expanded complexity allowances.

The native public baseline passed 823 tests and 124 subtests with 6 expected
failures in 55.87 seconds, but exited 1 because its default 100% coverage gate
reported 76.4% on the unmodified repository. The trial therefore discloses this
pre-existing failure and installed dependencies in a model-visible environment
note, while retaining the original feature requirements and the repository's
coverage settings. This preparation/prompt difference is explicit in the
manifest; no hidden-test or reference-solution information is given to the
candidate.


### Native trial found malformed calls becoming permission questions

The first launch preflight ended before inference because the copied executable
had lost its execute permission. Review also found the initial verifier Dockerfile
retained a second FROM line pointing to the corpus image; a corrected verifier
uses exactly the prepared native base. The corrected verifier checks Python's
build platform (`aarch64 linux-aarch64`), not just Docker's architecture label,
and its fresh `native-control-base` and `native-control-gold` controls pass at
0/159 plus 61/61, and 159/159 plus 61/61. The earlier image/build logs remain.
The paid native trial started at 21:28:07 UTC on runtime `8ae9d135e` using that
corrected verifier. Its own manifest and rig snapshots identify all inputs.

That frozen trial reached a permission card for a model-emitted bash call whose
arguments were only `{`. An unreadable command cannot be matched by approval,
so the defensive policy converted allow to prompt. This is safe against execution
but creates a product dead end: the person has no actual command to authorize.
The trial remains untouched, with that blocked state retained as evidence.

The next runtime rejects malformed/missing/empty/wrong-type bash commands before
asking approval, returns the shared actionable argument-error wording and counts
it as a model input failure. It never executes or guesses the missing command.
A corrected command still goes through the existing pre-action and approval
chain. The regression drives an invalid call, model correction, real shell run
and receipt of its output, without a consent event; the old code fails all five
invalid-input cases. Approval policy rules themselves are unchanged.

The native trial also exposed an overbroad environment hint: clearing every
pytest addopt disabled this repository's doctest/import setup and broke public
test collection. A new public baseline run verifies the narrower command
`python -m pytest tests/ -q -p no:cacheprovider --no-cov`, preserving repository
options and executing 823 passing tests, 6 expected failures and 124 passing
subtests in 33.49 seconds (exit 0). A later trial must disclose the corrected
hint as another experimental difference, not silently rewrite the frozen prompt.

The invalid-shell fix passes the final full session/approval/manual make-check
gate (181.859, 0.541 and 1.477 seconds), plus vet, formatting, packed manual,
build and size. Recovery, control-byte scrubbing and consent/denial/floor tests
pass three race repetitions (12.264 seconds). The first full run exposed one
old raw-control-byte fixture expecting invalid JSON to reach a consent card;
it now asserts no consent, no execution and a safe actionable error. Valid JSON
with an escaped control byte still exercises the existing card-scrubbing path.
The preceding deadline/build revision `8ae9d135e` passed both CI halves
(run 33992899226). The first native trial remains frozen at its permission card.
