# A prompt

The work is given as two words: *a prompt*. A prompt is a cue that signals
input is wanted. So the deliverable is a cue — a piece of text that, put in
front of the agent that reads this workspace, produces work rather than
questions. Below is that cue, written for the harness in this directory
(`cmd/codeaf`, the codeaf agent runner), and the one slot only the requester can
fill is marked rather than invented.

---

## The prompt

You are working in `cmd/codeaf`, the codeaf agent harness. Do the following,
in order, and stop when it is done.

**Subject.** {{SUBJECT}} — the change, defect or question this prompt is for.
If this slot is still a placeholder, stop and ask for it before touching
anything: a prompt with no subject is not this prompt but another one.

**First, orient — read, do not guess.** Establish three facts from the tree
itself, quoting file and line: what the current behaviour is, which test or
entry point already exercises it, and which law (a `*_test.go` that must not be
edited) constrains it. Prefer the closest existing test over a new one.

**Then, fix the behaviour, never the law.** Change the smallest production code
that makes the constrained behaviour true. If a law test fails, read it as the
specification and change the code, not the test. If the two genuinely
contradict, stop and say which law and which code, with the failing output.

**Then, prove it.** Run the narrowest command that exercises your change through
its ordinary entry point — the test that names it, or the binary as a user
reaches it, not a helper you wrote for the occasion. Report the exact command
and the exact output that came back. A green check you did not run is not a
check.

**Report.** In the reply: what changed and where, the command you ran, what it
printed, and anything you could not verify from here — naming the single short
check that would settle it. Do not narrate the steps you took; hand over the
result.

---

## What the two words did not say

Which subject, who asked, what medium, what deadline, and what "good" means were
absent from *a prompt*. The prompt above leaves the first of those as the one
thing the requester must supply and assumes the rest: the medium is Markdown in
this workspace, the agent is the codeaf harness, and "good" is a change that its
own tests exercise green. `prompt-analysis.md` in this directory holds that
reading in full; the prompt above is its usable form.
