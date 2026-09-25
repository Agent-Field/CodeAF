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
A team can override it for itself and the teams under it, on its card (the **teams page**);
with it off, a member's questions come to you as they always did.

The **Teams** tab of `/settings` holds the defaults every team inherits: `questions go to the
manager`, `team messages wake`, `daily cap per team`, `team depth` and `sub-team share`. Its
dim line says `a team can override any of these on its card · saved to your profile`. Over
`--host` the same five rows are the other machine's, and a change is saved there. Each value
says `from Settings`, the same words a team's card uses when it inherits them. An older
engine that cannot take the change keeps the tab read only and says
`changing them is not available over this connection`.

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

## Can a team cap be less than a cent

Yes. A cap is spelled as you set it everywhere it appears, on the card, the teams page and the
team's settings: `$5`, `$5.50`, and under a cent `$0.001`, never rounded to `$0.00`. **Raise
to** always offers twice the ceiling the team reached, and names exactly that figure: a
`$0.001` cap offers `Raise to $0.002`.

## Two windows ask once when a team reaches its cap

You are asked once for that team, that day, and that ceiling. A second codeaf window, or a
wake while the terminal is closed, finds the card already raised and adds nothing, so you do
not get two copies of `harbor reached its $5 cap today`. The next day asks again. The same
day asks again only after you choose **Raise to $10** and the team then crosses that new
ceiling.

## Wrapping up and closing a team

`Close…` on the teams page, `Close team…` on a team's card, or `D` on the conversations view
closes a team; with nothing running it closes at once and offers Undo (the **teams page** has
every path). Closing a team with work running offers **Wrap up first**. The manager is asked to tell every
member to finish the piece in hand and commit, to answer what it can, and then to bring you a
**closing report** with `team_close_report`: what was done, what is left, where the files are,
and what the team spent today. It arrives as a card with **Close** and **Keep going**, and the
team closes only when you pick Close.

The wrap-up has 15 minutes and $2 of team spend. When it runs out of either before the
manager reports, codeaf brings you the report itself, marked `wrap-up incomplete`, with
**Close now** and **Keep going**.

## How long does my team have left to wrap up

While a team is wrapping up, the teams page's header for it and the Traffic column beside its
manager say how long it has: `wrapping up · 12m left`, then `wrapping up · under a minute
left`, and `wrapping up · out of time` once the 15 minutes are gone and the report has not
come yet. The words go when the report arrives. The time is counted from when the wrap-up
began, kept with the team, so it reads the same after a restart. The team's chip on the tab
strip does not show it.

## What if the wrap-up report could not be sent, the decisions file was busy

A wrap-up that runs out of its 15 minutes or its $2 before the manager reports is
sent by codeaf itself, marked `wrap-up incomplete`, with **Close now** and **Keep
going**. If that write cannot take the decisions file because another writer still
holds it, the countdown stays due. The next look tries again. It does not wait for
a restart, and it does not send the report twice. A report that did go out clears
the countdown in memory and on disk, once.

## What happens to a wrap-up when codeaf restarts, does a wrap-up keep going if I quit codeaf

The countdown is kept with the team, in `teams.json`: the moment the wrap-up began, and the
15 minutes it was given. Quitting codeaf, or the engine restarting, does not drop it and
does not hand out another 15 minutes. The next time the manager's conversation is open, the
countdown continues with the time that is left. If that time already ran out while codeaf
was closed, codeaf brings the incomplete report once, the same way it does when the clock
runs out with codeaf open. Opening codeaf again does not bring that report a second time.
