# Adjudication: pilots 02 and 03, and the first interactive peers run

Three live runs the owner made against aforge `4d9d91fb0`, pinned to
`deepseek/deepseek-v4-flash-0731` throughout:

    /private/tmp/af-conversation-ops/live-pilot-02             print door, 4 scenarios × 3 arms
    /private/tmp/af-conversation-ops/live-pilot-03             the same, repeated
    /private/tmp/af-conversation-ops/live-interactive-peers-01 interactive door, pi and omp

This file adjudicates the non-pass cells in those runs. **Nothing in the raw
evidence has been touched**: no receipt is rewritten, no verdict is edited in
place. Where a recorded failure turns out to be this suite's fault, the fix is
in the rig and the row stands as it was written, explained here.

## 1. research-brief pi and omp (pilot-03): benchmark defect, replies correct

Both cells failed one assertion:

    says BRACKISH owns two services — /(two|2) services/ not found in reply.txt

Both replies say it. pi: "BRACKISH owns **two** services (kestrel and flange)".
omp: "BRACKISH owns **two** services (kestrel, flange)". The emphasis markers
sit between the number and the noun, so the phrase never matched. Across the two
pilots the same claim was written four ways — `owns 2 services total`, `owns two
services total`, `owns **2 services**`, `owns **two** services` — and only the
last failed, for its asterisks.

**Adjudication: both replies are factually correct on this point, and both cells
are benchmark defects, not quality failures.** The count is still required
exactly — `three services` fails — but the check now reads the text with inline
emphasis and code markers removed (`check_grep_plain`). A regression test uses
pi's actual reply shape, and a negative test uses the same sentence with the
wrong number.

This changes nothing about the rest of those two rows: the reconciliation check,
the four fact anchors and the owner name all passed on their own.

## 2. writing-memo omp (pilot-02): a real miss, of format

The memo's last line is `**Escalation:** If you are stuck ...`. The request was
"end with a single line beginning \"Escalation:\"", and the check is
`^ *Escalation:`.

**Adjudication: the miss is real and the cell stays failed, but it is a
FORMATTING miss and must not be read as a missing fact.** The escalation content
was there and the "escalation names the duty lead" check passed in the same run.
The check is now labelled `format: an escalation line begins literally with
"Escalation:"`, and a memo that emphasised the label records
`escalation_line=present but emphasised`, so the two kinds of miss cannot be
confused by a reader of the row.

## 3. writing-memo aforge (pilot-02): the guard vanished, not a product failure

What the evidence shows, in order:

* `18:52:04` — the cell is set up and its guard comes up; aforge starts, fetches
  the catalog through the guard (`guard-audit.jsonl` has exactly one row,
  `GET /v1/models?output_modalities=all`, upstream 200), and caches
  `model-catalog.json`.
* the first inference call is refused at the socket:
  `Post "http://127.0.0.1:64004/v1/chat/completions": dial tcp 127.0.0.1:64004:
  connect: connection refused`, after three retries.
* `guard-audit.jsonl.err` is **zero bytes**. A Python exception in the guard
  would have left a traceback there.
* `18:52:29` — the cell ends with no reply, no memo, and no `v3/usage.jsonl`.

**Adjudication: this cell is evidence about the rig, not about aforge.** The
harness never reached a model; every failed assertion downstream is a
consequence of that. The same cell passed on the repeat run (pilot-03).

Cause: not established. What the evidence rules out:

* **A subshell running the EXIT trap.** `run.sh` sets `trap 'guard_stop' EXIT`
  and the print door runs the harness in `( … )`. Tested on this machine's
  `/bin/bash`: the EXIT trap does **not** fire on subshell or command
  substitution exit, only once at shell exit. Ruled out.
* **The rig's own cleanup.** `arm_host_stop` ran after the failure, and its log
  for every aforge print cell says "nothing is holding … here" — print-door
  aforge left no session host to stop, so that path signalled nothing.
* **A guard crash.** No traceback, and an empty stderr is what an external
  signal looks like, not what an unhandled exception looks like.

What remains plausible and is not decidable from this evidence: an external
signal reaching the guard process, or the guard exiting for a reason it did not
record. Two changes make the next occurrence diagnosable rather than
misattributed:

1. `guard_stop` now captures the guard's exit status and whether it was already
   dead before the rig killed it (143 = this rig's own SIGTERM, 137 = SIGKILL,
   anything else = the guard leaving on its own).
2. `run.sh` checks the guard is alive when the cell ends. If it is not, the cell
   is **skipped** with the guard's status on the row, the scenario's checks do
   not run, and the row says the outcome is not evidence about the arm.

## 4. followup-while-working pi (interactive-peers-01): adapter calibration

Recorded as `crash` with ten failed assertions. The pane
(`followup-while-working-pi/screen.txt`) shows a healthy pi TUI whose status bar
reads `(guard) deepseek/deepseek-v4-flash-0731 • low`. The driver's ready marker
was the literal `\(openrouter\)` — calibrated before the guard existed — so it
waited out its 90 seconds and sent **zero** messages.

**Adjudication: a benchmark failure, not a product crash. The pi row in that run
carries no information about pi.** Two fixes: the provider segment is now
derived from the provider actually in use (`arm_provider_name`), and a screen
that is drawn but does not match the markers is recorded `noready` →
`unsupported`, with the scenario's own checks not run, instead of `crash`. The
`revision-midwork` pi cell in the same run has the same cause.

## 5. followup-while-working omp (interactive-peers-01): the pass is withdrawn

That cell passed, and one of its assertions should not have. The pane shows the
answer rendered as `RRABANNIC` — the derived token with a duplicated leading
character — and the check was a substring match, which `RRABANNIC` satisfies.

**Adjudication: the recorded pass is not safe and should be re-run rather than
quoted.** Whether the duplication came from omp or from the renderer is not
decidable here; the owner said they would look at the record. Either way the
check was too weak: the assertion is now a whole-word match, and a fake that
answers `RRABANNIC` is a selftest counterexample that must fail.

The rest of that cell stands: the work-phase witnesses show the followup sent
1.0s after the build started and the answer on screen 6.2s later, 54s before the
build finished.

## 6. omp loads the machine's MCP servers even under an isolated profile

The same omp pane shows `MCP finished with failures. Connected: node_repl,
openaiDeveloperDocs. Failed: aws-mcp [config: ~/.claude.json]`. The profile is
this run's own, but the MCP source reads the operator's home. pi's pane
similarly listed thirty-odd skills from `~/.agents/skills`.

That is extra tools, a longer system prompt and extra startup latency for two
arms and not the third, which makes the wall clocks less comparable than they
look.

**Adjudication: partly fixable with documented flags, and the remainder is a
recorded difference.** `omp --help` offers `--no-skills`, `--no-extensions` and
`--no-rules`, and `pi --help` offers `--no-skills` and `--no-extensions`; all are
now passed on both doors. For the user-level MCP import there is no documented
flag in omp 18.1.2 — `omp config list` offers `mcp.enableProjectConfig`, which
covers a project's own `.mcp.json` only — so it is still loaded, and every omp
row now says so in its `ambient` field. No dotfile of the operator's is
modified by this suite.

## 7. A cost row in pilot-03 that the merged meter would have caught

`research-brief omp` in pilot-03 carries `cost_source: self-reported`: its guard
usage file held no priced row for the cell, so the reader fell back to the
harness's own figure. Under the accounting merged since (`9e1900180`), an
admitted call that never settles makes the cell's cost **unknown** instead, and
a self-reported figure no longer stands in for a guard measurement. That row
should be read as cost-unknown.
