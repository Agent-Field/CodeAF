# Validation and remaining product work

This is a local development wave, not a release or a claim that the harness is on a
universal cost/time/quality frontier. The integration branch is
`santosh/conversation-runtime`; the shared `dev` checkout and the prior consolidated
QA branch were not used as mutable workspaces. Independent Claude Opus lanes were
reviewed and merged locally. After the Opus session limit, the owner authorized OMP
with GLM 5.3 for the remaining delegated review. All live comparison calls use
`deepseek/deepseek-v4-flash-0731`, including auxiliary roles. Guards refuse other
models before forwarding an inference request.

## Deterministic evidence

The code carries behavioral regressions for message provenance, delivery/reopen,
result preservation, task scope at admission, leaf capabilities, assignment revisions,
publication races, check authority, foreground handoff, host detach, status projection,
workspace identity and compaction. Focused tests, race checks and build checks were
run at lane integration. Final whole-tree validation is recorded below when complete.

Worker openings and landing instructions now use the runtime-note door. Previously,
a child admission could quote its parent's composed brief as a fresh statement by the
person. The new regression covers live recording, journal classification and actual
person corrections; composed instructions no longer become person quotes.

## Live observations

The manual terminal run `host-live-02` demonstrated foreground availability and work
continuing after terminal closure. It also found action replay by the checker, wrong
causality on a task-result wake, and relative paths misread as absolute. Each received
a bounded code fix and regression tests.

The nested run `host-live-03` tested source candidate `b8e868d85` through the normal
hosted terminal. Task 1 created task 2, the prepared one-minute action ran once, and
the parent produced the marker and correct service count. Explicit checks did not
repeat the action. Admission took about 77 seconds, including two missing-deliverable
refusals, one invalid piped check and provider stall rescue. The child still did some
unassigned counting and read a contradictory manual passage; that passage and the
composed-brief provenance defect were fixed afterwards.

The main conversation answered during the build in about 8.7 seconds, but the saved
model response reversed the checksum incorrectly. This is a quality failure. The
parent result was then recorded, but its automatic narration encountered a provider
internal-markup cut; no substantive final answer appeared during observation. A task
card and a stored artifact do not by themselves satisfy the conversational delivery
requirement. The guard recorded interrupted and unresolved calls, so a complete cost
for this diagnostic run is unknown.

The first integrated paired-run attempt was invalid: a long evidence path exceeded
Aforge's Unix socket path limit, causing in-process fallback and setup. The rig now
uses a short owned alias to the same cell state. Those two unsupported cells are not
product failures or successful tests.

## Comparison limits

The small print-task pilot was not consistently faster or cheaper than Pi or OMP.
Its two orderings and a few tasks do not establish a frontier. Early cost metering
could omit interrupted requests; current metering reconciles every admission and
reports unknown cost when usage is missing.

Peer interactive evidence uses one repetition per case with a 60-second prepared
command. Pi answered the follow-up after the command finished. OMP displayed the wrong
word. Both peers honored the output-format correction. OMP's installed MCP discovery
was still present despite disabled skills, rules and extensions, weakening isolation.
These facts are observations, not a ranking of the harnesses in general.

## Corrected hosted comparison, candidate 0c76ea5ca

The corrected run used the short state alias, the same 60-second fixture and the
same exact DeepSeek model as the peers. Both Aforge cells failed. Follow-up: 86s total,
incorrect `RABBANIC` in the model response and no build-result narration on screen.
Revision: 183s, valid CSV produced but superseded Markdown also left behind. The
fixed session goal was visibly trying to restore the Markdown requirement after the
person changed it. Multiple build starts overwrote a phase marker and also spoiled
its ordering witness. The fixture now counts invocations and keeps the first start.
Both cells have unknown total cost due missing upstream usage; token receipts were
155,235 input / 2,560 output and 772,819 input / 23,523 output respectively.

These failures prompted two additional bounded repairs: subscribing hosted sessions
to runtime-started turns, and separating interactive conversation policy from a frozen
headless goal. Their final validation belongs below once integrated; the failed raw
runs remain evidence and are not relabeled as passes.

## Hosted stream follow-up, candidate f7e341d65

The wake subscription alone did not pass the live follow-up case: 148s, $0.005210415,
259,894 input and 5,122 output tokens. Unlike the preceding run, the journal contains
the correct `RABANNIC` answer at 01:05:24Z and the complete build result at 01:06:29Z.
Neither survived visibly in the captured chat. Hosted turn adoption reused the
previous turn number and could discard a new stream while the old tail was draining.
The fixed-goal controller also ordered a second invocation of the prepared action.
This is a failed product run; correct hidden model output is not a passed chat test.

The display repair routes hosted turns through the existing local follow-up/wake
queue. Two deterministic regressions fail before the change and pass afterward:
preserving a preceding answer across a hosted wake, and retaining a new hosted stream
while the previous one drains. The focused TUI suite passed in 2.888s. The remote
wake subscription passed its full package under the race detector in 20.497s, plus
structural laws and the full manual package. Full integrated validation follows.

## Interactive policy candidate 1e1756b6f

The mid-work revision case passed: the action ran once, the change arrived while it
ran, all three CSV rows were correct, and the superseded Markdown file was absent.
Wall time was 104s. Total cost is unknown (36 of 40 calls priced); the receipt records
399,746 input and 15,909 output tokens. This fixes the observed reversion behavior,
not a general proof of correct steering across arbitrary tasks.

The follow-up case still failed: 94s, $0.002813903988, 138,511 input / 3,746 output.
The action ran once. The model first returned an incorrect word, then corrected it
after the work finished. Its build answer was hidden by the later correction inside
the same turn. The trace exposed a remaining causal gap: background command outcomes
had no task result tag, so the completion reader fell back to the latest unrelated
question. Explicit background outcome provenance now survives note batching and
selects its own reply duty. Untagged control wakes retain their prior behavior.

The shared prompt also now applies to general conversation and artifact work, keeps
engineering guidance conditional on coding, and asks for deterministic computation
when exact calculations or transformations matter. Redundant uncertainty wording was
shortened to keep the existing fixed-prefix budget unchanged. The follow-up rerun
below evaluates these changes; this failed candidate remains in the evidence.

## Priorities after this wave

1. Make result delivery an explicit user-facing obligation through provider failures:
   retained result, attempted narration, visible retry/failure, and a clear way to
   recover. Model text ending is not proof that the person received an answer.
2. Reduce admission overhead using measured traces. Keep a small typed assignment;
   avoid requiring title, summary and prose restatements as independent model-authored
   sources of truth. Measure malformed-tool recovery and model calls before work starts.
3. Replace command-count heuristics for foreground work with a policy that accounts
   for duration and reversibility. A long single command can occupy chat as effectively
   as a hundred edits; a quick commit still should not need a worktree ceremony.
4. Finish scoped clarification and revision propagation to dependencies. A person can
   address one task now; broadcasting to all descendants would be wrong when only one
   deliverable changed. Questions need correlated owners and affected work needs explicit
   revision dependencies.
5. Extract larger packages only after the interfaces settle. Delivery, provenance,
   admission, verification authority and status have single owners, but the session
   package still has direct internal access. Landing/workspace disposition and evidence
   evaluation remain the next useful module boundaries, with compatibility tests.
6. Run a broader randomized and repeated battery: research, writing, data analysis,
   multiple repositories, long context, interruption, crashes, revisions and return
   after absence. Report quality gates, time to first useful result, total time, actual
   billed cost and failed-call cost. Do not optimize only the successful cells.
