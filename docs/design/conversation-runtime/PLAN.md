# Aforge: keep thinking while the work gets done

**Product proposal · 2026-09-04**

The product I would build is a conversation that stays useful while decisions turn into results. The person should be able to discuss a direction, say “build that,” continue thinking, occasionally look inside the work, change their mind, and return later to something they can use.

The visible innovation is continuity across simultaneous work. Faster agents alone do not produce it. Neither does putting a task dashboard beside an ordinary coding transcript. Aforge should absorb the coordination that currently falls on the person: repeating context, checking whether anything started, saying “continue,” reconciling results, and reminding workers what changed.

This plan starts from that experience. Existing task graphs, prompts, branches, checks and room designs do not define the answer. The prior audit supplies failure examples; it is not the specification. The owner confirmed that requested work must continue after the terminal closes.

## 1. The product's unit is an outcome someone can discuss

The person's objects are “offline reading,” “the launch draft,” “the pricing comparison,” and “that import problem.” Those are more stable than an agent process, model name, implementation plan or sequence of tool calls.

Each substantive assignment has a conversation attached to it. The main conversation keeps the overall intent; the work conversation holds the investigation, implementation and local decisions. A task may create further work conversations, but remains responsible for its own outcome. Opening any one of them means joining the same ongoing effort, with its existing context. It does not launch a new assistant that asks for the brief again.

The product should feel like **one assistant with several threads of work**, rather than several assistants the person must manage. Models and workers can change underneath a stable assignment. A work conversation can survive retry, checkpoint, model replacement and reconnect without changing its identity.

A task is not obliged to spawn more tasks. One capable worker is the default useful case. Delegation is something that worker can do when it finds a reason, not a ceremony every request must pay for.

## 2. The main conversation stays at the level of intent

The main chat is where the person asks questions, explores possibilities, chooses direction, starts work and understands outcomes. It can read a relevant file or look up a fact to have an informed conversation. It should not disappear into a hundred-step implementation while the person is waiting to discuss the next idea.

Implementation, lengthy investigation and artifact production move into work conversations. Very small work can appear as a compact operation inside the main exchange; it should not require a planner, a separate naming call and a checklist before starting. The execution record can still exist underneath. Separating work from the foreground must become cheap enough that it does not tax every minor change.

Do not make users choose between a chat mode and a work mode. Interpret the request in context:

| Person says | Expected behavior |
| --- | --- |
| “Could offline reading make this more useful?” | Discuss the idea |
| “What would it take to add it?” | Assess; investigate in the background if substantial |
| “Can you add offline reading?” | Start the work; polite phrasing is still a request |
| “Let's do the simpler version we discussed.” | Start the agreed version using the existing context |
| “Maybe we should support teams eventually.” | Continue exploring; do not silently expand the running implementation |
| “Change the running version to support teams.” | Revise that assignment and coordinate the consequences |

This needs model judgment, not keyword rules. Ask a short clarification when two plausible readings would cause materially different effects. Do not ask merely because an internal tool wants another field.

The assistant should acknowledge an actual start with a concrete sentence: “I'm adding local offline reading. I'll bring back a working preview.” The interface can draw a task receipt immediately after admission. It must not wait for a naming model, and it must not claim something started before execution was accepted.

## 3. Work becomes part of the conversation

A handoff creates a compact work item at the point it was discussed. Its title names the outcome. Below it are short, attributable lines about current work, a question if necessary, and eventually the result.

A conceptual example, not a pixel design:

```text
You
Let's add offline reading. Keep this version local to the device.

Aforge
I'm adding local offline reading. I'll bring back a working preview.

  Offline reading                                      running
  Saving articles and checking how saved pages reopen
  Open work

You
While that's happening, what should the onboarding explain?

Aforge
It needs to explain what is available offline and how to remove it...
```

Activity should remain visible in the conversation. A jobs-only sidebar is insufficient: work otherwise seems to disappear. Within a work item, a few short lines may show independent activity with attribution. Changes update that item's live area rather than adding a new assistant message for every grep or child completion. The person can expand the details.

A small optional active-work strip keeps older assignments reachable after the conversation moves on. It is an aid to navigation, not the main product. A permanently expanded graph would make the person think about implementation topology before they need to.

Meaningful developments deserve a new conversational update: “Private browsing prevents storage here; I'm using an in-memory fallback.” Routine progress does not. Completion should appear once at the level that owns the user's request, not once per descendant.

No update steals keyboard focus, moves the person away from what they were reading, or destroys a draft. Someone reading earlier messages can see that new work information exists without being dragged to the bottom. Announcements for assistive technology should favor meaningful state changes over every caption update.

## 4. Entering work is joining a conversation

Opening “Offline reading” shows:

- Its current aim, expressed in one or two sentences.
- The latest useful finding and what is happening now.
- Its draft, preview, files or other output when available.
- The ongoing conversation, including the person's earlier steering.
- Child work when it helps explain progress or a dependency.

The composer is an ordinary conversation composer. “Why this approach?”, “show me the current version,” “try the simpler cache,” and “stop here” all work without special modes.

A question about status should not cancel execution. Opening a task does not pause it. Leaving does not resume it, because it never stopped. A user message is addressed to the ongoing worker; inspection does not secretly spin up a second worker that mutates the same material.

A compact path such as `Main / Offline reading / Storage` makes the current addressee clear. Back returns to the main conversation at the previous reading position with its draft intact. The person never has to summarize what happened inside the task on the way out.

The latest preview is more important than the tool log. Evidence and detailed tool history remain available, but the first view answers “what is being made, how is it going, and can I change it?”

## 5. Changing your mind is a normal operation

The crucial innovation is that steering actually updates the work. It is not a note appended beside a frozen plan that a later checker ignores.

Default scope follows where the person speaks. In a work conversation, direction applies to that assignment and its descendants. In a child, direction applies there. In the main conversation, named work and clear conversational references identify the target. Explicit words can widen the scope from anywhere.

| Message | Product response |
| --- | --- |
| Inside storage: “Use IndexedDB for this.” | Apply to storage; notify the owner of consequential interface changes |
| Inside storage: “Actually, the entire feature must work without a server.” | Update the feature constraint; inform affected work |
| In main: “Make both drafts more direct.” | Update both named drafts |
| “What if we used SQLite?” | Discuss; do not assume an implementation change |
| “Use SQLite instead.” | Apply the change and explain any material consequence |

A clear local instruction from the person outranks an older agent plan. The parent cannot later overwrite it simply because the original brief said something else. If the instruction conflicts with another explicit user requirement, resolve the concrete conflict rather than pretending recency answers every question. “Including email addresses would replace your earlier no-personal-data requirement for this export. Is that the change you want?” is a meaningful question. “Which node should receive this?” is usually the system outsourcing its job.

For an ordinary revision, acknowledge the effect: “CSV replaces JSON for this export. I'm updating validation too.” The person can inspect that acknowledgment or correct it. Do not require approval of a generated contract form.

Distinguish receipt from completion. “Change received” means it is stored; “working with the new format” means the worker has applied the direction to its next work. Already-running operations may take time to stop. The interface should say so accurately. A private draft created under the old direction may still be useful evidence, but it must not silently become the delivered result for the new direction.

If a completed result is being changed, continue the same work conversation with a new revision and keep the prior result accessible. A genuinely new assignment can start a new linked task. “Continue” should not recreate the task or ask for the story again.

## 6. Most coordination should not involve the main chat

The main chat is not the manager of every worker turn. The task owner decides how to accomplish its assignment, delegates when useful, answers its children's questions from available context, and integrates their work. It can do useful work itself while children run.

Children share concise findings, questions, interface changes and results. They do not broadcast transcripts. The parent is accountable for resolving incompatible assumptions. The user should not paste one child's answer into another or approve a decomposition just to get moving.

Use an ownership tree for responsibility and explicit dependencies where work crosses that tree. The organization may evolve as facts emerge. A task can discover a problem, delegate a small investigation and continue another part; it need not invent the whole graph before reading the material. Cyclic waits and duplicate ownership are runtime errors to prevent, not puzzles to give the user.

A useful early finding can change ongoing sibling work. For example, a storage investigation discovers a browser limit; the owner adjusts the downloader immediately rather than waiting for every child to finish. An ordinary successful subtask need not wake the owner if there is nothing yet to decide. The distinction is material news versus bookkeeping.

The main conversation needs only changes that affect the user's understanding, decisions or next request. It can fetch details when asked. It should not reread or re-approve every child's successful step.

## 7. Questions should earn the person's attention

Assuming capable models means trusting them to resolve ordinary implementation choices. Questions are for missing preferences, facts or authority whose wrong interpretation materially changes the outcome.

A child asks its parent first when the parent could know the answer. It reaches the person only if necessary. Related questions can be combined into one understandable choice. The question includes a sensible recommendation when one exists and a concrete consequence. It never includes an internal role dispute.

A question visible in main and in the task is the same question. Answering in either place resolves it once. If the person is away, it remains available on return. Independent work continues. A required answer is not silently invented after a timeout.

Not every uncertainty needs to block. For a reversible detail within the requested scope, the worker can choose a reasonable default and note it with the draft. For a decision that would invalidate substantial work, asking is cheaper and kinder. The product should learn enduring preferences only when supported by the user's decisions, not promote one local choice into a permanent global rule.

## 8. Results are the return path

“Task finished” is not an adequate result. Return what the person wanted:

| Assignment | What comes back |
| --- | --- |
| Feature | Working preview or actual code delivery, changed behavior and relevant checks |
| Bug fix | Reproduced failure, corrected behavior and regression evidence |
| Research | Answer or recommendation with sources and unresolved uncertainty |
| Data work | Output, reproducible calculations and relevant assumptions |
| Writing | The draft itself, ready to read or use |

An outcome should be understandable from the main conversation without opening a task. Details expand into the original work. A concise return might be: “Offline reading is ready in the preview. Saved articles reopen without a connection. Images are cached; embedded video still needs a connection.”

The words must match the actual delivery stage. A preview ready for review is not a production deployment. A kept branch is not a merged change. For work that lacks an objective pass/fail check, say what was completed and assessed rather than manufacturing certainty.

New ideas can grow directly from results: “Keep the behavior, but make the save control smaller.” That message continues the relevant work with its existing evidence and artifacts. The person should not specify task IDs or manually establish the dependency unless the reference is genuinely ambiguous.

## 9. Closing the terminal changes presence, not intent

The owner explicitly chose continued execution after closing the terminal. The terminal is therefore a view into work owned by a persistent local service.

Closing the window detaches. Requested work continues under its existing scope and budget. It does not create new goals or acquire additional authority while the person is away. A pending required question stays pending; other useful work can continue.

Returning restores the same conversation and work. Offer a short recap such as: “Offline reading is ready. The launch draft needs your choice of audience.” Link to the results and questions in place; do not replay the entire activity feed.

Local work cannot execute while the machine is powered off. Sleep pauses local execution; reconnect/restart should recover safely when possible. If an external action may have completed before a crash, reconcile its actual outcome before repeating it. A remote execution host can later provide availability independent of the laptop, but it is not an invisible promise of the local design.

“Pause this,” “stop this,” and closing the window have different effects. Pause preserves resumable work. Stop prevents further work where possible and preserves useful partial results. It does not undo effects that already happened. The person can stop one assignment, its family or all active work from the main conversation without hunting for processes.

## 10. The minimum information model behind the experience

These records exist to make conversational behavior reliable, not to become forms in the UI.

- **Conversation:** user messages, relevant decisions, linked work, and outcomes. Preserve exploratory ideas as exploratory.
- **Assignment:** current aim, relevant constraints, evidence of completion, owner, scope, budget and revision.
- **Working context:** useful observations, hypotheses, source/artifact references, current dependencies and unanswered questions.
- **Result:** usable answer/artifact, delivery state, checks performed, remaining limitations, and the revision it satisfies.

Keep the distinction between the person's decision, an observed fact, and a model's interpretation. Context given to a worker should answer “what am I responsible for, what matters here, what is already known, and how can I get more detail?” It should not be the entire main transcript or every ancestor's complete goal.

Compaction is a memory-management detail. It may shorten working history, but must preserve the current assignment, binding directions, open questions and useful results. Sources remain retrievable. Changes to a file can make an earlier observation stale. Sometimes reading it again is the correct behavior.

Do not build a universal knowledge graph first. A few typed records, links to original messages and source revisions are enough for the initial product. Version changes that affect scope or acceptance; do not make every sentence a formally adjudicated fact.

## 11. Assume capable models; keep deterministic obligations in code

The model should decide how to solve the work, when a helper is useful, what evidence is sufficient, and which uncertainty deserves a question. The harness should reliably deliver messages, enforce scopes and budgets, preserve outputs, keep the foreground responsive, and report what happened.

That division removes many unnecessary AI management layers. A task should not need separate models to name it, restate the brief, approve the restatement, classify every step, narrate it, and then independently infer whether a completion string means success. Use an appropriate model for real judgment. Use code for state transitions and bookkeeping.

One conversation/worker loop can serve both main and task threads. Do not require a new framework for nested work. A task becomes another addressable conversation with an assignment, a durable state and the ability to delegate.

The minimum runtime is a persistent owner process, transactional task/message storage, a bounded execution queue and durable result delivery. A waiting conversation consumes no model call. Current records plus append-only messages are sufficient; a full event-sourced architecture is not a prerequisite. Diagnostic traces are separate from the authoritative user/work records.

Cap actual executing requests and tools, not just visible tasks. Preserve capacity for foreground interaction and urgent steering. Budgets include descendants, coordination, retries and checking. Extra depth must consume a shared allowance rather than create new money at every level.

Isolation depends on effects. Code changes need safe ownership and integration. Read-only investigation does not need a new Git repository. A document task needs an output location and versioning, not a branch ritual merely because coding tasks use one. Cross-task writes must have an owner or an integration step; conversation alone cannot prevent overwrite races.

## 12. What I would deliberately leave out

- A mandatory plan/task-definition form before work starts.
- A graph dashboard as the default home of the product.
- A requirement to select models, assign worker counts or approve internal decomposition.
- A separate “talk” versus “execute” mode that the person must remember.
- Every-child completion announcements and raw tool traffic in the main chat.
- Automatic scope changes inferred from speculative ideation.
- A permanently running manager model that polls workers.
- A universal reviewer pass for every small deliverable.
- A blanket ban on rereading or a requirement to treat inherited summaries as truth.
- Unbounded recursion as a selling point.

Available detail should expand when the person wants control. Expert controls remain reachable, but expertise should not be the price of ordinary success.

## 13. Criticizing this proposal

**I initially overemphasized machinery.** My audit recommendation—contracts, evidence stores, explicit events—could become another large architecture with an equally demanding interface. I would reduce it to the smallest records needed for delivery, steering and recovery. Product behavior is the test; a beautiful state model is not evidence of a good experience.

**A main conversation can become too vague.** If it is insulated from all work, it will offer generic advice and forget what is possible. It needs current outcomes, material discoveries and reliable access to details. It does not need the entire tool stream.

**Quiet can become invisible.** Hiding everything behind a sidebar makes the person wonder whether work is happening. Keep short meaningful activity within the conversation's work items, with visible changes and easy expansion. Reduce noise without removing evidence of life.

**Delegation can make work slower.** A strong single worker with parallel tools may beat several agents because it shares context and avoids integration. Ship the same product experience for both. Increase internal parallelism where paired measurements show benefit; never make the user perform the optimization.

**Fluid steering can destroy coherence.** If every local thought changes everyone else's instructions, work becomes unstable. Scope follows the active conversation, material shared changes reach the owner, and speculative discussion stays distinct from directions. A real conflict between user requirements deserves a focused question.

**One owner can become a bottleneck.** The owner should handle consequential coordination and final integration, not approve every child tool call. Children retain local autonomy. Code handles ordinary scheduling and delivery. The owner sleeps when it has nothing useful to do.

**Changing direction has a real cost.** Do not pretend a rewrite is free. When a new choice invalidates substantial work, say what will be reused and what must change. Do not force the person to approve every minor adjustment to protect a fragile plan.

**“Just works” cannot mean confident claims.** For deterministic work, check the result. For qualitative work, use meaningful review at the appropriate scope and expose uncertainty. A capable model should be given responsibility, with the product preserving observable evidence rather than surrounding it with ritual distrust.

**Persistent autonomy can feel out of control.** Show what continues, keep stop/pause straightforward, and maintain the agreed scope and budget. The service executes requested work; it is not a self-appointed employee inventing assignments.

## 14. A full experience to test before committing to the design

1. The person discusses offline reading and chooses a local-only first version.
2. “Build that” starts an assignment in place. Main remains available immediately.
3. The person explores onboarding while the task works. No speculative onboarding idea changes the implementation accidentally.
4. The owner discovers two independent pieces and delegates. A short activity area shows work advancing without an agent-management ceremony.
5. The person opens storage, asks why it chose a mechanism, and says “keep this browser-only.” The change is recorded and affects the right work.
6. A storage finding changes downloader assumptions. The owner coordinates it without asking the person to carry messages.
7. The person returns to main and requests a short help draft. That work proceeds independently.
8. The person closes the terminal. Both assignments continue locally while the machine is available.
9. One task encounters a preference it cannot resolve; the question persists while independent work finishes.
10. On return, the person sees a short recap, answers the question, and receives a usable feature and draft.
11. “Make the explanation shorter” revises the correct draft without restarting its research or altering the feature.

Repeat the same shape for a multi-source research comparison, data analysis with a changed definition, and a writing task with an audience change. If this experience requires the person to learn internal terms, restate context, collect child outputs, or repeatedly say “continue,” the product has failed even if all its workers ran successfully.

## 15. Delivery plan, organized by product proof

| Stage | Build | Required proof before expanding |
| --- | --- | --- |
| 1. Handoff and return | Main conversation plus one persistent work conversation, inline work item, usable result | Commission ordinary work, ask an unrelated question, leave, return to the right result without another prompt |
| 2. Enter and steer | Same live work opens as chat; scoped revisions; receipt/application feedback; pause/stop | Direction survives active calls and checking; returning preserves draft/location; result satisfies latest request |
| 3. Capable delegation | Same worker loop delegates bounded child work; ownership, questions and integration | Parent resolves useful child news; user need not manage siblings; one coherent delivered outcome |
| 4. Continuity under change | Cross-task consequences, recovery, pending questions, previous results and revisions | Resume without duplicated effects; concurrent directions do not silently overwrite; stale outputs cannot become current results |
| 5. Reduce waiting and cost | Parallelism/routing/context refinements using the same experience | Faster accepted outcomes with equal or better quality and no added supervision |

Build the first stage with one capable worker. It proves most of the product promise and avoids using agent-count as a proxy for innovation. Design delegation semantics to support recursion, but roll out deeper nesting only after the simpler configuration succeeds and there is evidence it helps. Stage 3 should include at least one genuine child-of-child scenario before claiming arbitrary nested steering works.

Each stage is a vertical slice: UI, runtime, persistence, outcome checks and observable traces together. Do not implement an elaborate backend and postpone the conversation experience until the end.

For the existing repository, evaluate reuse of the provider/tool loop, terminal renderer, task conversation views and local engine host against these requirements. Replace competing lifecycle and steering paths rather than preserving them because they already exist. Keep old sessions readable; opt new sessions into the new runtime, use versioned storage and retain a rollback route. Do not dual-run mutating work during migration. Every changed behavior updates the compiled manual in the same implementation wave.

## 16. How we decide whether this is actually better

The primary measure is **a usable result with less required attention**. Track:

- Whether the work met the latest request and actually reached its destination.
- Time until the person can continue talking, and time to accepted result.
- Required clarifications, repeated instructions and manual coordination.
- Correct interpretation of exploration versus a direction to act.
- Whether steering arrived, was applied, and changed the right scope.
- Time spent by the person inspecting status just to know if work is alive.
- Total spend and integration/retry overhead, including all descendants.

Qualify behavior first with deterministic UI scenarios for interruption, concurrent messages, child questions, changed scope, unavailable providers, restart and stale results. Use actual produced artifacts and recorded lifecycle facts, not prose claiming success. Retain failing evidence automatically.

Then compare one persistent worker, a worker using parallel tools, bounded helpers, and recursive helpers on matched workloads. Hold model/effort/provider policy steady for the orchestration comparison. Separately test model allocation. Include independent and tightly coupled work, changed requirements, coding, research, data and writing. Use paired repetitions and blind qualitative assessment where needed; do not report a speedup from one favorable run.

A high-level user walkthrough must pass without opening the task tree. A second walkthrough deliberately enters a child and changes its direction. Both must work. The point is that control is available, while supervision is optional.

## Recommendation

Build the conversational handoff-and-return experience first, with one persistent capable worker and a task conversation you can enter. Make steering a real change to the work and make delivery unambiguous. Then let that same work conversation delegate when useful.

This keeps the product ambitious while reducing implementation uncertainty. The person gets the experience they want from the first slice; increased parallelism improves it behind the same interface instead of becoming a new product they have to operate.
