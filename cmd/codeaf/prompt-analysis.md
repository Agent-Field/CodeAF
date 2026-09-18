# Reading the two words

The work was given in full as *a prompt*. This file is the reading that
`prompt.md` is the usable form of. It follows the shape a two-word brief forces:
what the words state, what they leave open, what follows from the form, and the
one slot only the requester can fill.

## What the words state

"A" is an indefinite article: one instance of the noun following, unnamed and
unowned. "Prompt" names a cue — something put in front of an agent that causes
it to act, in the ordinary sense of a stage prompt or a shell prompt (cited
because the text itself defines nothing). Quoted whole the phrase asserts that
some cue exists or is wanted, and asserts nothing besides that. It names no
agent to be cued, no subject for the work, no medium, and no reason. Nothing in
the two words can be true or false and nothing in them can be executed.

## What the words leave open

- **Subject.** What the cued agent is to work on. Absent, and this is the one
  item the requester alone can supply: inventing a subject would produce a
  different prompt, not this one.
- **Who asked.** Who is answerable for the result and for the cost of running
  it. Absent.
- **Medium.** Whether the cue is prose, a file, a chat turn, a command. Absent.
- **Deadline and size.** How long the cued work may take and how much it may
  spend. Absent.
- **What "good" means.** The condition under which the cue has done its job.
  Absent. The cue written in `prompt.md` adopts one reading — a change exercised
  green by the harness's own tests — and names it rather than assuming it.

## What follows from the form

A prompt is not a plan and not an answer: it is the input that produces them.
It therefore has to be written so the agent reading it will act rather than ask,
which means three things. It must carry a slot for the one fact only the
requester holds, and say what to do if the slot is still empty. It must fix the
method the agent is to use, because the two words supplied none. And it must
state what counts as proof, because "done" is otherwise the agent's own word.

`prompt.md` does all three: `{{SUBJECT}}` is the requester's slot with an
explicit stop if it is unfilled; the method is read-then-fix-the-code-not-the-
law; and the proof is a command run through the change's ordinary entry point,
with the exact output handed back. The rest the words did not say — the medium
(Markdown in this workspace), the agent (the codeaf harness in `cmd/codeaf`),
and "good" (the change's own tests green) — is stated in that file as a choice,
not smuggled in as a fact.

## Why this form and not another

The alternative readings all fail the same way. Read as *the word "prompt"*, the
brief asks for a definition and can be answered by a dictionary. Read as *a
file named prompt*, it asks for any file and is satisfied by an empty one.
Read as *a plan*, it produces a document the requester did not ask for. The
reading taken is the one where the deliverable is what the words denote: a cue
that causes work. That cue is `prompt.md` in this directory.
