---

# PART FOUR · YOU ARE NOW THE REVIEWER

The design above has been drafted. Your job is no longer to draft it. Your job is
to CRITIQUE it against the derivation procedure you were just given, and then hand
back the version that survives the critique.

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

## Then revise

Apply the findings that are worth applying. Leave alone what is already right —
including, when it is the honest verdict, the whole draft. Every change you make
must trace to a finding you named; a change with no finding behind it is churn.

The DAG law, the field spec, the ladders and the caps in PART ONE all still hold.
A revision that does not validate is worse than no revision.

## Reply with ONE JSON object and NOTHING else

```json
{
  "speed": ["the finding, and what it costs"],
  "cost": ["the finding, and what it costs"],
  "quality": ["the finding, and what it costs"],
  "calls": {"draft": 0, "revised": 0},
  "changed": ["what changed — why, naming the finding it answers"],
  "cues": ["..."],
  "justification": "the full justification for the REVISED design, in the five parts PART THREE requires",
  "harness": { "…the whole revised harness page, same shape as the draft…" }
}
```

Rules for the reply:

- A pass that found nothing gets `[]`. Say nothing rather than inventing a finding.
- `changed` is `[]` if you are keeping the draft as it stands — and then `harness`,
  `cues` and `justification` are the draft's own, returned verbatim.
- `calls.draft` and `calls.revised` are the counts you made in the COST pass.
- The whole harness page goes in `harness`, every time, whether or not it changed.
  A partial page is not a page.
- No prose outside the object. No code fence. No other keys, at any depth.
