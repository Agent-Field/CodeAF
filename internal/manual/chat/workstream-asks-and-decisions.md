# Asks, views and decisions in a workstream

## who decides an Ask — exactly one decider, approval by several people

An Ask is a question that blocks a task, and it has exactly one decider. A task's own Ask is decided by the task's owner; only the owner gets its buttons. When several people must approve something, the task carries several Asks, each with its own decider, and the task waits on every one of them: answering one leaves it waiting until the last is answered. There is no Ask that several people decide together, and no shared owner — that deadlocks or races, so it is not built. An Ask nobody answers for a day also reaches the workstream's fallback person.

## add your view to someone else's Ask — a view is not a vote

On an Ask you do not decide, the card has no buttons for you; it says who decides and offers a field, "Add your view for Marco". A view is one short line for the decider to read before answering. Each person has one view per Ask; writing again replaces yours, and clearing it takes it away. The decider never adds a view to their own Ask — they answer it. A view is not a vote: the decider reads the views and decides. When the Ask is answered, its decision says whose views were attached.

## the Decisions list — what records itself, and where to find it

Decisions is the first row in a workstream's Files. It opens the list the workstream builds itself; nobody writes it. A decision is recorded when:

- an Ask is answered (made by whoever answered it);
- a request changes shared work — the request's one line on the task, and what it settles (made by whoever asked);
- a rule is set in People and rules (made by whoever set it).

Each row is the decision in one line, who made it, where it came from, and when. All or Mine filters the list. A click on a row opens the task or Ask it came from. It is the workstream's shared memory: how one person's agents know what another person decided. Agents read it with `ws_decisions`.

## withdraw or replace a decision — only the person who made it

Only the person who made a decision can withdraw it or replace it, from its row's menu in Decisions. The old one stays in the list, greyed, with who replaced or withdrew it and when ("Replaced by Marco on Nov 9"). Nobody else sees those choices on your decisions, and the server refuses them from anyone else: "only the person who made a decision can withdraw or replace it". Nothing is deleted.

## why was my task paused — the decision check, a conflict asks whoever made the decision

Before a new task starts, and before an agent calls its work done, the app checks the work against the workstream's decisions with your chosen model. When it goes against one — a price change after "No price change in 2.4", a birthday field after "We don't collect birthdays" — the work is paused and an Ask goes to the person who made that decision, never to anyone else. The Ask says which decision and how the work goes against it. The task is not done, nothing runs around the decision, and the agent tells you who was asked. Work that keeps to a decision, or only mentions its subject, passes; when the check is unsure, it lets the work through. A workstream with no decisions is never checked.

## Keep the decision or Go ahead anyway — answering a conflict

A conflict Ask offers two answers. "Keep the decision" is recommended: the decision stands, nothing new is recorded, and the work goes back to be done within it. "Go ahead anyway" lets the work through and records an exception in Decisions, made by the person who answered. To change the decision itself, its maker replaces it in Decisions.

## Assumed — what the agent decided without asking, and Send back

Ask before, or do and tell after. Cheap, reversible choices that go against no decision an agent makes itself, and lists them on the task under "Assumed · Done without asking", one line each ("Left out two fixes that only affect the team"). Anything else it asks first, as an Ask on the task. Anyone in the workstream can overturn an assumption with "Send back…" beside it: you say why, and finished work goes back to whoever did it, with "instead of …" and your reason in its thread. That is the only way to overturn one; a comment does not.
