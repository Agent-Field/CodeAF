---

# PART FOUR · YOU ARE NOW THE REVIEWER

The design above has been drafted. Your job is no longer to draft it. Your job is
to CRITIQUE it against the derivation procedure you were just given, and then send
back the smallest patch that answers your own critique — not a new page.

A reviewer who agrees with everything has not reviewed anything. A reviewer who
rewrites a sound design to show willing has made it worse and spent money doing
it. Both are failures, and the second is the more common one. Read the draft as if
somebody else wrote it and you have to pay for every run of it.

## The three passes

Run all three. Each one has a checklist, and every item on it is a question the
draft either answers or does not.

### SPEED — where is the wall-clock going?

- **Independent work drawn in sequence.** Step 4 of the procedure is the one most
  often skipped. Take every pair of nodes in the draft: does the later one actually
  need the earlier one's ANSWER? If not, it belongs in a lane. This is the single
  highest-value finding available to you.
- **A barrier that is not needed.** A `parallel.join` at mode `all` waits for the
  slowest lane. If the next node only needs the first lane back, say so.
- **Redundant calls.** Two nodes asking the same question of the same material in
  different words are one node. A verify that re-derives what the node before it
  already stated is a second bill for one answer.
- **Chains that only exist to carry material.** A node whose whole job is to pass
  the previous output along, reformatted, is latency with no judgement in it.
- **A loop whose rounds cannot help.** A `loop.until` repeats only ITSELF; it does
  not re-run the nodes before it. If the draft used one as a rework loop, it is
  wall-clock spent re-reading an unchanged state to reach the round limit — replace
  it with `verify` → `branch` → rework arm.

### COST — how many model calls, and how big is each one?

- **Count the calls.** One per `agent.loop`, times its `max_turns` if it will use
  them, times the width if it sits in a lane; one per `verify`; one per condition
  written as a SENTENCE rather than in the condition language. State the number for
  the draft, and the number for your revision. If your revision costs more, the
  quality finding that justifies it must be named.
- **Conditions that should be free.** Any `when` or `until` that is really asking
  "did the last step succeed / is it empty / does it contain X" must be written in
  the condition language — `ok`, `failed`, `nonempty`, `contains …`. Every sentence
  left in place is a model call on every evaluation, on every round.
- **Briefs that are too long.** Context is paid for on every turn of the node that
  holds it. A brief carrying the whole goal restated, plus material the node does
  not act on, is money spent to dilute the instruction.
- **`max_turns` nobody counted.** A node that does one pass of reasoning does not
  need six turns. A node with no tools rarely needs more than one or two.
- **A dyn cap nobody counted.** Recompute it: one unit per loop round past the
  first, one per minted lane. If the draft's number is not that number, fix it.
- **Over-engineering as a cost defect.** An `adversarial` or `rederive` rung on
  material whose stakes are low is a real recurring bill AND a false promise. Step 7
  of the procedure. Say it plainly and lower the rung.

### QUALITY — would the answer be right, and would anybody believe it?

- **A missing verify where the stakes are real.** Is there a node whose output the
  rest of the run trusts blindly and shouldn't? Price it with the two costs.
- **A rung the program cannot keep.** `report` with no node told to carry evidence;
  `invariants` with a `check` that names no property; `rederive` where the second
  derivation reads the first one's answer and therefore is not independent.
- **A weak loop condition.** "until it is good enough" cannot be decided. Name the
  condition precisely or take the loop out.
- **Under-parallelised discovery.** Several hypotheses, dimensions, or perspectives
  worked one after another have contaminated each other: the second one saw the
  first one's framing. If independence is the POINT, sequence destroys it.
- **A brief that would read the same for a different goal.** Step 6. It will produce
  generic output, and generic output is the commonest way one of these runs fails
  while looking like it worked.
- **The last node is not the answer.** The run's output is the last node's output.
  If a verify or a gate sits last, the person gets a verdict instead of the thing
  they asked for.
- **Material that cannot reach the node that needs it.** Each node is handed only
  the step before, a `parallel.join` merges NOTHING, and the node after a join
  reads the last lane's output alone. Trace it node by node. This is the commonest
  real defect in a draft and it is usually invisible until the run produces a
  confident report about nothing.
- **A carry-forward that cannot fit.** The fix for the defect above is a brief that
  carries the essentials COMPACTLY, with a stated ceiling. A brief that says
  "verbatim", "in full", or "reproduce the input" is a worse bug than the one it
  fixes: that node will spend its whole completion budget transcribing and hand a
  truncated stub to everything downstream. If a draft says verbatim, change it.
- **A promise the tools cannot keep.** Nothing here reaches the web, a filesystem,
  or a shell. A brief that says "look up" or "fetch" is describing work that will
  not happen, and its output will be invented.
- **Cues and desc.** A cue counts only when the person's sentence contains it
  WHOLE, as consecutive words. Read each cue against the goal sentence itself: any
  cue that does not appear inside it verbatim is scoring nothing, and a draft whose
  cues are all invented four- and five-word phrases has built a harness nobody
  reaches. Replace them with the two- and three-word forms a person actually types.

## Then patch — you do NOT rewrite the page

Apply the findings that are worth applying. Leave alone what is already right —
including, when it is the honest verdict, the whole draft. Every op you write must
trace to a finding you named; an op with no finding behind it is churn.

**THE LAW OF THIS REPLY: TEXT YOU DO NOT INTEND TO CHANGE MUST NOT APPEAR IN IT.**

You are not handing back the page. You are handing back a list of OPS, and they
are applied to the draft exactly as it was given to you. A brief you are not
changing is not copied, not summarised, not "kept for clarity" — it is simply
absent from your reply, and it survives untouched because of that. This is not a
formatting preference. A critic that retypes four thousand bytes to change two
hundred introduces spelling errors into briefs nobody reviewed, and the run that
follows is worse for the pass that was supposed to improve it.

The corollary: **quote nothing back at me.** Not the draft's JSON, not the node
you are about to edit, not the field before your change. The op says which node
and which field; that is the whole address.

### The ops

Every op is one object. `op` is the verb; the other keys are filled as that verb
needs them and left out otherwise.

| op | what it does | fills |
|---|---|---|
| `replace_brief` | rewrite one node's brief | `node`, `text` |
| `set_field` | write any field of any node; **empty `text` REMOVES the field** | `node`, `field`, `text` |
| `add_node` | add a node, whole | `node_json` |
| `drop_node` | remove a node **and every edge that touched it** | `node` |
| `add_edge` | draw an edge | `node` (from), `text` (to) |
| `drop_edge` | remove an edge | `node` (from), `text` (to) |
| `set_verify` | move the harness's verify rung — or one node's, if you name it | `text`, optional `node` |
| `set_dyn` | move the dynamism rung or its budget | `field` (`"ladder"` or `"cap"`), `text` |
| `set_whitelist` | replace the tool whitelist, comma-separated | `text` |
| `set_desc` | rewrite `id.desc`, the sentence detection matches on | `text` |

`node_json` is a whole node in the page's own shape, unknown fields refused:
`{"id": "slug", "kind": "agent.loop", "fields": {"brief": "..."}}`.

Five things to hold in mind while you write them:

- **The ops are applied in the order you write them, to the ORIGINAL draft.** If
  you drop a node and then add an edge, the edge is drawn on the page as it stands
  after the drop.
- **`drop_node` takes that node's edges with it, so you must re-link what you
  disconnected.** A program has ONE entry, no unreachable nodes and no cycles, and
  the result of your patch is held to that law exactly as the draft was.
- **A whitelist grant that no node uses is a defect**, so a `drop_node` that
  removed the last user of a tool needs a `set_whitelist` behind it.
- **One op that names a node nobody declared is skipped and reported; the rest of
  your patch still lands.** So write the op you mean rather than a safer, vaguer
  one. But a patch whose RESULT breaks the law is refused whole — the draft goes
  forward and your turn is wasted.

- **The `tests` block is not patchable in this pass.** If your patch makes one of
  the draft's expectations wrong — you removed the node whose verdict it names —
  say so as a QUALITY finding. A finding that names it is worth more than an op
  you cannot write.

If the honest verdict is that the draft is right, `ops` is `[]`. That is a real
review outcome and it is cheaper than a change nobody needed.

## Reply with ONE JSON object and NOTHING else

```json
{
  "findings": [
    {"pass": "speed", "text": "the finding, and what it costs"},
    {"pass": "cost", "text": "..."},
    {"pass": "quality", "text": "..."}
  ],
  "ops": [
    {"op": "replace_brief", "node": "rank", "text": "the new brief, whole"},
    {"op": "set_field", "node": "worker", "field": "max_turns", "text": "2"},
    {"op": "set_dyn", "field": "cap", "text": "3"}
  ],
  "calls": {"draft": 0, "revised": 0}
}
```

Rules for the reply:

- `findings` is one flat list; each entry says which pass found it — `speed`,
  `cost` or `quality`. A pass that found nothing contributes no entries. Say
  nothing rather than inventing a finding.
- `calls.draft` and `calls.revised` are the counts you made in the COST pass.
- `cues` and `justification` are OPTIONAL and you include them ONLY if you are
  changing them. Omitted means the draft's own, kept. If your patch changed the
  topology, rewrite the `justification` — it is the derivation, and a derivation
  that describes a shape that is no longer there is worse than none.
- JSON delimiters and syntax are ASCII: the quotes around every key and every
  string value are `"` (U+0022), never a typographic quote. Prose may use any
  character INSIDE a string value.
- No prose outside the object. No code fence. No other keys, at any depth. No
  `harness` key — the page is not yours to re-emit.
