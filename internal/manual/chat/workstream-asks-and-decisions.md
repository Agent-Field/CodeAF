# Asks, views and decisions in a workstream

## who decides an Ask — exactly one decider, approval by several people

An Ask is a question that blocks a task, and it has exactly one decider. A task's own Ask is decided by the task's owner; only the owner gets its buttons. When several people must approve something, the task carries several Asks, each with its own decider, and the task waits on every one of them: answering one leaves it waiting until the last is answered. There is no Ask that several people decide together, and no shared owner — that deadlocks or races, so it is not built. An Ask nobody answers for a day also reaches the workstream's fallback person.

## ask someone a question on a task — Ask someone, ask a teammate to decide, answer Yes, No or Reply…

You can ask one person in the workstream to decide something on a task, the same way Afi asks you. On the task, open More, choose "Ask someone" and pick the person; only people in the workstream are listed, and never you. The box below becomes "Ask Marco": its first line is the question and the rest is what they need to know to answer ("Your question for Marco · the first line is the question"). Press "Ask". The toast says "Asked Marco · it waits in their Inbox until they answer".

The task now waits on that Ask. The card says "You asked Marco" to you, "Priya asks you" to Marco and "Priya asks Marco" to everyone else, and the thread says "Priya asks Marco: <question>". Marco sees it under Waiting on you in their Inbox ("Priya asks you") and on Work. Only Marco answers it: "Yes", "No", or "Reply…", which opens a field for an answer in words; Return sends it. A person's Ask has no "Tell it…", because there is no agent to tell. An Ask that names its decider goes to that person even when it is the task's first question: it does not fall back to the task's owner. Naming the owner makes it the task's own Ask. Their answer is kept in the workstream's Decisions as the question and his answer ("Can the two crash fixes land before the 28th? Yes"), and the task goes back to what it was doing: up next stays up next, and Afi's work carries on with the answer.

Limits, in the server's words: someone outside the workstream is refused ("Lena is not in this workstream, so they cannot answer its Asks; ask someone who is"); you cannot ask yourself ("an Ask goes to someone else; you decide your own questions"); and a done task takes no Ask ("this task is done, so nothing waits on an answer; comment and @ them instead"). Ask someone is on an organization's workstreams; Personal has nobody else to ask.

## answer Afi's question in your own words — Reply… on an agent's Ask

An Ask from Afi (or another agent) on a task page offers its choices, then **Tell it…**, **Reply…** and **Later**. **Reply…** opens one field under the card, labelled "Reply to Afi · Return sends it": what you type answers the Ask itself, the same as picking a choice. The Ask closes, your words are kept in Decisions as `Asked “<question>”, answered: <your words>`, and the task carries on with them. Use it when no choice fits, or to paste what the agent asked for (the release notes it could not find). **Tell it…** instead opens the conversation with the question quoted, for talking it over; it does not close the Ask. Only the person who may answer the Ask sees either. A task your Personal Factory runs as a job answers its question on the job instead, and has no Reply….

## can be undone — a reversible Ask, and who marks it

Every Ask says whether its answer can be undone. Whoever raises it says so: Afi and chats mark an Ask "can be undone" only when what the answer lets them do can honestly be undone with nothing lost but a little time (a wording, a draft, an order of work); anything published, sent, paid for, deleted or against a decision is never marked. An Ask a person raises is not marked. Marked Asks gather in the Inbox under "Quick, and can be undone", where "Accept Afi's answer on all" answers them together, and each such answer can be undone from its task until the task is done; the Ask opens again and its decision is withdrawn.

## add your view to someone else's Ask — a view is not a vote

On an Ask you do not decide, the card has no buttons for you; it says who decides and offers a field, "Add your view for Marco". A view is one short line for the decider to read before answering. Each person has one view per Ask; writing again replaces yours, and clearing it takes it away. The decider never adds a view to their own Ask — they answer it. A view is not a vote: the decider reads the views and decides. When the Ask is answered, its decision says whose views were attached.

## the Decisions list — what records itself, and where to find it

Open Decisions from the Overview side block or the first row in a workstream's Files. It opens the list the workstream builds itself; nobody writes it. A decision is recorded when:

- an Ask is answered (made by whoever answered it). An answer picked from the Ask's choices keeps the choice's words; an answer in words alone is kept as `Asked “<question>”, answered: <your words>`, so a question that names a problem is never recorded as if the problem were settled;
- a request changes shared work — the request's one line on the task, and what it settles (made by whoever asked);
- a rule is set in People and rules (made by whoever set it);
- another workstream is folded into this one: "Marco folded “Launch swag” into this workstream" (made by whoever folded it; Undo removes it).

One decision is one row: the same words from the same person, while that decision is still kept, are not recorded again, so answering several identical Asks together, or one request noted on six tasks, keeps a single decision, and the decision check asks its maker about it once. A rule is recorded each time it is set. Each new answered Ask shows its original question with the answer underneath, who answered, when, and where it came from. Older records that did not keep those words show the saved decision. Search matches questions, answers and the source. This week and Earlier group the results; All or Mine filters the list. A click on a row opens the task or Ask it came from. It is the workstream's shared memory: how one person's agents know what another person decided. Agents read it with `ws_decisions`.

## withdraw or replace a decision — only the person who made it

Only the person who made a decision can withdraw it or replace it, from its row's menu in Decisions. The old one stays in the list, greyed, with who replaced or withdrew it and when ("Replaced by Marco on Nov 9"). Nobody else sees those choices on your decisions, and the server refuses them from anyone else: "only the person who made a decision can withdraw or replace it". Nothing is deleted.

## why was my task paused — the decision check, a conflict asks whoever made the decision

Before a new task starts, and before an agent calls its work done, the app checks the work against the workstream's decisions with your chosen model. When it goes against one — a price change after "No price change in 2.4", a birthday field after "We don't collect birthdays" — the work is paused and an Ask goes to the person who made that decision, never to anyone else. The Ask says which decision and how the work goes against it. The task is not done, nothing runs around the decision, and the agent tells you who was asked. Work that keeps to a decision, or only mentions its subject, passes; when the check is unsure, it lets the work through. A workstream with no decisions is never checked. In a Personal workstream you are the only person who can answer, so the Ask is yours even when the decision was made by someone else (say, Priya, from a meeting note): the card names Priya as the decision's maker, and the agent tells you it asked you. On an organization the server's Afi is checked the same way, so the same Ask reaches the decision's maker whoever's Afi did the work. A document an agent is about to write into shared Files is checked the same way; one that goes against a decision is not written, and the agent makes a task for the work instead, which pauses on that Ask.

## Keep the decision or Go ahead anyway — answering a conflict

A conflict Ask offers two answers. "Keep the decision" is recommended: the decision stands, nothing new is recorded, and the work goes back to be done within it. "Go ahead anyway" lets the work through and records an exception in Decisions, made by the person who answered. To change the decision itself, its maker replaces it in Decisions.

## Assumed — what the agent decided without asking, and Send back

Ask before, or do and tell after. Cheap, reversible choices that go against no decision an agent makes itself, and lists them on the task under "Assumed · Done without asking", one line each ("Left out two fixes that only affect the team"). Anything else it asks first, as an Ask on the task, unless the workstream lets Afi decide that kind of question without asking: then it is listed under Assumed too, saying so. Anyone in the workstream can overturn an assumption with "Send back…" beside it: you say why, and finished work goes back to whoever did it, with "instead of …" and your reason in its thread. That is the only way to overturn one; a comment does not.

## An answered Ask keeps the question and answer separate

A words answer is recorded as “Marco answered ‘the question’: the answer”, rather than a choice of an option. The task thread keeps the question separate from the answer. Decisions shows the question above the answer, followed by the person's display name and when it was answered. The organization's connection preserves these fields on their way to the desk.
