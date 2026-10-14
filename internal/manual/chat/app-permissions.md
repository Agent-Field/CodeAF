# App questions, permissions and always-allow rules

## App permission cards: Allow once, Deny and Always

The app reads each permission from the binary as an Ask. Answers from the card
and the Inbox share one five-second Undo hold. After that hold, Allow once
releases that one command; Deny sends a refusal to the agent so it can continue
without running it. Both leave a receipt where the question arose.
The card carries the command, why it asks, and two context entries from before
the question. Those entries stay fixed when more conversation arrives.

Always saves a rule for exactly this command or the command class the binary
shows, and for this agent or all your agents. Exact means literal equality:
a `*` in the command is not a rule wildcard. A class answers only one whole
shell command; it cannot silently authorize a compound command. Some commands
have no class to save. Irreversible permissions never offer Always and refuse
an attempt to save one: "irreversible actions always ask".

## Allow for this task: one yes for a task's read-only commands

When a command only reads, the permission card offers "for this task" as its
first scope, before the two saved rules. Choose it and codeaf stops asking about
that program's other read-only commands until the task ends. In a conversation
with no task, it lasts until the reply ends; the next message asks again.
It saves no rule, so nothing appears in Settings and there is nothing to remove.

Only a plain read-only `gh` command counts today: `gh issue view` and `list`,
`gh pr view`, `list`, `status`, `diff` and `checks`, `gh repo`, `release`, `run`
and `workflow` `view` and `list`, `gh label list`, and `gh api` reading one path.
The command must stand alone; one trailing ` 2>&1` is the only addition that
still counts as a read. A pipe such as `| head`, any other redirect, a chain
with `;` or `&&`, quotes, a variable, or a flag codeaf does not know makes it
an ordinary command again, and it asks every time. So does every command that
writes: a comment, a merge, a `gh api` call with a method or a body. Commands
on the always-ask list and irreversible ones never offer this scope; an attempt
answers "only a read-only command can be allowed for this task".

Read-only commands an agent sends in the same step arrive as one card that
lists each command. One answer covers all of them. A command that writes in
that step keeps its own card. While a command waits for your answer the task
counts it as a step, so a waiting task never reads "0 steps".

## App always ask list and Don't ask mode

Settings → Permissions keeps "acting in your name", "deleting outside a task"
and "payments" in Always ask. These entries cannot be removed. Extra entries
may name a tool, an exact command or its shown command class. These actions
still ask in Don't ask mode, with a saved rule or with an account set to Yes.
A rule that explicitly denies an action continues to refuse it.
Software-install and virtual-environment commands on the bash permission floor
also always ask, including pip installs, global npm installs, brew installs and
downloaded scripts piped from curl to sh or bash. Saved rules cannot allow them
automatically; an attempt to save one is refused with "irreversible actions always ask".

## App always-allow receipts, Why, Remove and Undo

A saved rule applies to later calls through the binary's permission gate. It
leaves "Allowed automatically" in the conversation, attributed to codeaf,
with a link to its rule. Why reads the rule and the command it matched.
Settings lists active rules. Remove disables the rule immediately; restoring
it for Undo keeps the same identity and scope. Rules and receipts survive an
app restart. Saving a rule never creates a separate tool-wide session memo,
so removing the rule also removes its permission for the next call.
A refused answer leaves an existing saved rule active and creates no answer receipt.

## App permissions batched from a task

Permissions from the same task share a batch. The task waits on those commands;
the conversation can keep working. Allow all and Deny all answer the currently
waiting rows. One by one answers only the selected rows. The binary checks all
submitted rows before sending any answers, including their task and agent.
An irreversible row never accepts Always, even inside a batch.

## App clarification: Send, Later and You decide

A clarification accepts your words. A choice accepts one of its listed options.
Later holds the question without answering it and leaves a Later receipt.
You decide gives the suggested answer back to the asker; a clarification with
no suggestion tells it to choose a reasonable approach and continue. A choice
without a suggestion refuses delegation. An irreversible ask needs your answer.

Answers given in the Inbox or another window reach the same engine resolver
and leave a receipt here. A second answer is refused with the original answer's
attribution and time; it is never sent to the model. A question that is no
longer needed leaves "No longer needed — the turn moved on".

## App question clock: no answer in 15 minutes pauses the agent

A blocking permission or question starts a 15-minute clock by default. Settings
can choose 1–1440 minutes. The binary owns the timer; leaving the tab does not
hold it. Later holds a question or permission's timer. Submitting an answer also holds
the timer during the five-second Undo window; Undo leaves it held. Expiry stops
the agent's work without sending Allow, Deny or the suggested answer, and leaves
"Paused · no answer before the question clock ran out". The engine withdraws
pending calls cancelled by that stop. Send a new message to continue the work.
Irreversible permissions can time out into a stop; they never time out into
permission. Task proposal clocks and questions that block nothing are separate.
