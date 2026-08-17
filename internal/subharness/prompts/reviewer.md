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
draft either answers or does not. After them come two DUTIES, which are not
questions: they name a trigger you can see in the draft, and when the trigger is
there your reply must say something about it.

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

## The two duties — neither may end in silence

Everything above is a question you may answer "nothing found". These two are not.
Each names a trigger that is READABLE IN THE DRAFT, and when the trigger is there
your reply must carry either the ops that answer it or ONE LINE saying why the
draft is right as it stands. Silence about a triggered duty is the one review
outcome that is always wrong.

They are written out rather than left to the checklists because both defects have
already survived a review that found other things. Neither is visible to the
derivation check, and neither looks untidy on the page.

### DUTY ONE · FAN-OUT CANDIDATE — one node doing three jobs

TRIGGER: a node whose BRIEF enumerates three or more items that EACH carry their
own piece of work. "each approach", "all three", "every subsystem", "the four
dimensions", "for each hypothesis" — a list inside one brief, worked by one
worker, in one thread.

The test for the trigger, and it is a real test rather than a word count: hand a
competent person item two ALONE. Is there still a job in front of them — something
to price, investigate, evaluate, design — and would they rather do it without
having seen item one's answer first? Both yes is a fan-out candidate.

NOT A FAN-OUT CANDIDATE, and no finding is owed for it: several outputs of ONE
act. Three taglines, three names, five bullet points, a paragraph in three parts.
Item two there is not a job, it is a line, and three lanes to write three lines
buys three contexts and three model calls to save nothing. That is the
over-engineering this guide spends most of its words against, and this duty is
not a licence for it.

Read every brief in the draft for a list. Nothing else catches this. The SPEED
pass's first item compares PAIRS OF NODES and so does the derivation table, so
three jobs collapsed into ONE node leave no pair to disagree with: the table says
`depends` — truthfully — about the nodes that remain, every check in the system
passes, and the three evaluations run one after another inside one context with
each one contaminated by the last. That is the failure the whole guide is written
against, arriving in the one form no machine here can refuse.

WHEN YOU FIND ONE, propose the split. The ops, in this order:

1. `add_node` a `parallel.split`: `width` is the number of items, `over` names
   what the lanes are over.
2. `add_node` one worker per item, each brief written for ITS item alone and
   naming it — not "evaluate the approach" but which approach, and against what.
3. `add_node` a `parallel.join`.
4. `drop_node` the collapsed node. It takes its edges with it, which is why the
   relinking is next.
5. `add_edge` from whatever fed the collapsed node to the split; from the split to
   each lane; from each lane to the join; from the join to whatever it fed.

Each added node travels in `node_json`, in the page's own shape:
`{"op": "add_node", "node_json": {"id": "fan", "kind": "parallel.split", "fields": {"width": "3", "over": "the three approaches"}}}`,
then the lanes, then `{"id": "gather", "kind": "parallel.join", "fields": {"mode": "all"}}`.

Three things this shape needs, or the patch is REFUSED WHOLE — and a refused
patch means the draft you criticised goes forward unimproved and your turn bought
nothing:

- **The ladder.** A `parallel.split` is a `width` kind. A draft sitting at `fixed`
  or `branch` must move with it: `set_dyn` `ladder` `width`, and then `set_dyn`
  `cap` — one unit per minted lane. The ladder op comes FIRST, because a `fixed`
  harness may not carry a cap at all.
- **The join merges nothing.** The node after the join reads the LAST lane's
  output alone. So the last lane's brief is the one that must carry the other
  lanes' results forward, COMPACTLY, with a ceiling you state — a line each, a
  number each. Never "verbatim", never "in full".
- **The justification.** You changed the topology, so rewrite it. A derivation
  that describes the shape you just removed is worse than none.

WHEN YOU KEEP IT SERIAL, write one line in the `speed` pass naming the node and
the reason: real shared state each lane would have to rebuild, a genuine
dependency between the items (item two is priced against item one's number), or
items so small that carrying context into three lanes costs more than the
wall-clock it saves. "It reads fine" is not a reason. What is forbidden is
silence: a draft with an enumerating brief in it and a review that never mentions
that node has skipped the thing most likely to be wrong with it.

### DUTY TWO · STRANDED VERIFY — a check with nowhere to fail to

TRIGGER: a `verify` node whose FAILURE has no outgoing path of its own. Take each
verify in the draft and ask what runs when it says FAIL. Three answers are the
same defect:

- nothing, because the verify is the last node;
- the same node that runs when it passes, because the verify has one successor
  and the run walks straight through the verdict;
- a `loop.until`, which re-reads its OWN condition and re-runs nothing before it,
  so the work the check rejected is never redone.

The run dies with the work done and paid for. The person asked for a decision and
receives the word "failed". This is the more expensive of the two defects, because
every model call before it was spent correctly — and a check that cannot change
what happens next was a bill with no consequence attached.

WHEN YOU FIND ONE, draw the fork:

1. `add_node` a `branch` immediately after the verify, with `when` in the
   condition language — `failed`. A sentence here is a model call on every run to
   decide something the state already knows.
2. `add_edge` to the REWORK arm FIRST and the onward arm second: the first
   successor is the arm taken when the condition holds.
3. `add_node` the rework arm: one worker, briefed with what the check objected to
   and told to hand back the corrected deliverable itself — not a report about it.
4. `add_edge` from the rework arm to the node that delivers, so both arms end at
   the deliverable and the run's last node is still the thing that was asked for.
5. `set_dyn` `ladder` `branch` if the draft is at `fixed`.

THE CAP IS THE DRAWING. There are no back edges in this format and a `loop.until`
repeats only itself, so a "capped rework" means an arm drawn FORWARD a fixed
number of times — one rework node, or rework then a second `verify` and then the
deliverable. An op that edges back to the verify draws a cycle, and a patch whose
result is a cycle is refused whole.

WHEN DEATH IS THE ANSWER, write one line in the `quality` pass saying so: this
verify IS the gate on the deliverable, a person reads its failures, and a run that
reworked its way past the gate would ship the thing the gate exists to stop. That
is a real verdict, and a `human.gate` earns it too. What is forbidden is a review
that names neither the branch nor the reason there isn't one.

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

`set_verify` moves a rung UP or SIDEWAYS, never down. Lowering a rung is how a review quietly spends the design's safety to buy simplicity, and it is the one change a critic is not allowed to make: if the draft's rung looks too strong, say so in findings and leave it — the person approving the card decides to spend their own safety. Raising a rung is always welcome, with the reason named.
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
- THE TWO DUTIES ARE THE EXCEPTION, and they are not inventions. A draft with an
  enumerating brief in it contributes a `speed` entry naming that node — the
  fan-out, or the reason it stays serial. A draft with a verify in it contributes
  a `quality` entry naming that verify — the failure path, or the reason death is
  the answer. One entry per node that tripped the trigger, and the entry names the
  node so the line can be checked against the page.
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
