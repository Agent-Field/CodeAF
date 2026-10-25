# Optional learning during ordinary work

This is a proposed integration contract, not an installed production prompt.
Normal work remains the primary task. Learning proposals use context the agent
already has, without a second full-history extraction pass.

## Working-agent instruction

Complete the user's task and write the normal useful response. At a meaningful
checkpoint, you MAY also emit a small `knowledge_delta` in the structured response
envelope. Omit it or use an empty list when nothing durable changed. Do not invent
a lesson to satisfy a quota or turn routine progress into memory.

Notice a definition that became clear, a procedure that actually worked with its
prerequisites, a decision and its tradeoff, an explicit correction, an unresolved
incompatibility, or a missing explanation that blocks useful action. Explain the
subject, not who said what. Preserve intent versus completion and proposals versus
observations. Tool success can support only what that result actually established.

Propose at most four items. Each item has an operation (`learn`, `correct`,
`procedure` or `question`), short useful text, a semantic subject hint, and exact
source quotations. Use existing source IDs supplied in your context; `self` may
refer to an exact span of your normal response after it is committed. Do not cite
hidden reasoning, future output, model background knowledge or an unseen source.
A correction names the existing claim ID it may replace and explicit evidence;
an assistant's reinterpretation cannot override a human decision.

Questions must identify a concrete missing prerequisite or mechanism and what
answering it would unlock. Do not ask questions already answered in your context.
Put them in the proposal; do not interrupt the user or start a researcher unless
the main task warrants it. No company/project names, folder taxonomy or mandatory
subject checklist is built into this instruction.

The proposal is NOT an accepted memory. Do not edit shared notes or claim that
memory was saved. The compiler checks scope, references, duplicates, corrections
and semantic support before any durable knowledge change.

## Envelope

```json
{
  "response": "The normal user-facing response.",
  "knowledge_delta": [
    {
      "op": "learn",
      "text": "A concise durable proposition with its conditions.",
      "subject": "A subject inferred from current work",
      "evidence": [{"source": "self", "quote": "Exact text from the response"}]
    }
  ]
}
```

`scope` and owner/project permissions are assigned by the host from trusted task
context, never by the model. The collector binds `self` to the committed response
ID and validates quotations before staging. Questions remain questions; procedures
and claims remain proposals; a source match is not a truth check.

## Delivery and fallback

For providers that support it, stream the `response` field through the normal
renderer and collect the optional structured delta separately. Persist the source
response before binding evidence. A malformed or absent delta must not fail the
user's task. The experimental collector never extracts a footer from arbitrary
source prose: quoted markup or code can otherwise masquerade as metadata.

Provider/streaming compatibility requires binary work and measurement before
integration. A dedicated nonblocking proposal tool is another adapter only if it
can finish without a compulsory extra model turn. Do not add a second completion
merely to save a proposal, and do not hide an extra research loop in a Stop hook.
Legacy providers can use a debounced fallback extraction over new committed
content. Capture all source events so a missed proposal does not permanently lose
knowledge. Use sampled omission checks to measure the working-agent proposal path;
agent silence is not evidence that nothing useful was learned.

## Why this changes cost

A small repeated instruction and extra output tokens still cost money. This is
not free memory. The potential saving is that the working agent does not need to
reread its own history in another completion; unchanged/duplicate proposals can
be staged locally with zero additional model calls. New or ambiguous proposals
share a bounded compiler context across concurrent tasks. Expensive explanation,
contradiction repair and curiosity work runs only for affected subjects.

Record proposal bytes/tokens, accepted useful knowledge, omissions, compiler and
review charges, queue lag and failure rate. Compare checkpoint sidecars against
full extraction on the same work before choosing a default.
