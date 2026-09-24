# Team questions, decisions and caps

## Where a member's question goes

In a team with a manager, a member's clarifying question goes to **its manager first**, not to
you. When the member asks with `ask` (a question it needs answered to go on: which of two
shapes, what a word means, whether to go ahead), nothing appears on your screen: the question
becomes a **decision packet** for the manager it reports to, and the member is told it went
there. The manager answers it with `team_decide`, the member is handed the answer marked
`◆ answered: …` and carries on, and the traffic rail shows `answered @web: …`. A member that
was idle is woken by the answer.

When the manager cannot or should not answer, it sends the question up with `team_escalate`:
to its own manager, or to you. It reaches you only when no manager above could decide it.

**Permission prompts never go up.** A member asking to run a command or edit a file asks you,
always, and no manager can answer that for it.

This is the setting `questions go to the manager` in `/settings` under **Teams**, on by default.
A team can override it for itself and the teams under it; with it off, a member's questions come
to you as they always did.

## What a decision packet is

Everything that has to be decided above the conversation that met it travels as one packet you
can answer without reading a transcript: the question, who raised it and what each side said,
the options with what happens if each is chosen, and a recommendation with its reason. The same
packet is answered by a manager or by you. Packets waiting on you are in the **inbox** on the
teams page, one card each, with the options as buttons and `Your own answer…` for words of your
own. A packet waiting on a manager shows as `waiting on ◆ harbor`, and you can still decide it
yourself: you outrank every manager.

A manager's own questions reach you the same way: as a packet addressed to you, in the inbox,
when it has no manager above it. Your answer is handed to the manager marked `◆ answered: …`
and wakes it.

Packets are kept in the profile of the machine the conversations run on, in
`teams/<id>/decisions.jsonl` for the team each was raised from. Past a megabyte the file starts
a new one, keeping every packet still waiting.

## A team's daily cap

A team can have a daily cap: `daily cap per team` in `/settings` under **Teams** is the default
every team inherits (no cap), and a team can set its own. A cap counts the team and every team
under it together, one pool.

When the pool reaches its cap:

- nothing new starts in it: a directive no longer wakes a member, replies no longer wake the
  manager, a new member's brief waits, and `team_start` is refused. A turn already running is
  never cut off; it finishes, and what was held is delivered at each conversation's next turn;
- you are asked once, with a card `harbor reached its $5 cap today`: **Raise to $10** (the
  team goes on until $10 today) or **Stop for today** (members finish their current turn and
  start no new one until tomorrow), with a recommendation;
- your own messages in a member's conversation are never held; the cap is on the work the team
  starts by itself.

A manager can never raise a cap: money is yours. Every held wake is one line in the traffic,
`held @web: harbor reached its $5 cap today`.

## Wrapping up and closing a team

Closing a team with work running offers **Wrap up first**. The manager is asked to tell every
member to finish the piece in hand and commit, to answer what it can, and then to bring you a
**closing report** with `team_close_report`: what was done, what is left, where the files are,
and what the team spent today. It arrives as a card with **Close** and **Keep going**, and the
team closes only when you pick Close.

The wrap-up has 15 minutes and $2 of team spend. When it runs out of either before the
manager reports, codeaf brings you the report itself, marked `wrap-up incomplete`, with
**Close now** and **Keep going**.
