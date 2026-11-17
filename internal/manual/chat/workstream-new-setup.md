# Starting a new workstream — Afi asks what it is for, then proposes the setup

## new workstream — how do I create a workstream, what happens when I press +

Pressing **+** beside Work (or File › New Workstream, or New workstream on Home) makes the workstream at once, named
"New workstream", and opens it with Afi's pane on the right. There is no form first. Afi's first message is already
there:

"What's this workstream for? Tell me in a sentence or two: what you want done, who's involved, and any date that
matters. I'll draft the setup for you to change."

You answer in your own words. Pressing + again while that new workstream is still untouched (no answer to Afi, no
tasks) opens the same one again rather than making a second empty one. The same happens in an organization's window,
where the workstream is the organization's from the first moment and its people see it named "New workstream".

## Afi's questions when setting up — why did Afi ask me something, how many questions

Afi proposes as soon as it can. When what you said leaves something it cannot guess and that changes the setup (who
decides, a date), it asks at most two short questions, in one message. It never asks about rules, spending, names or
files: it drafts sensible ones and you change them on the card.

## the workstream card — Afi's draft, what is on it, editing the setup before it exists

Afi's proposal arrives as a card in the main column, headed "Afi's draft · not set up yet". Every line is a value you
can change in place:

- **Name** and **Goal** (what done looks like, in a sentence or two).
- **People** and teams to add (an organization only). A name nobody in the organization answers to is shown dim, "not in
  <organization>", and is left out.
- **Who answers**: Incoming work (who owns work nobody asked for directly), Decides (who looks after the workstream:
  its people, rules and archiving), and If unanswered for a day (the fallback). Decides and the fallback are an
  organization's only.
- **Rules**: Start without asking (the most an agent may spend on a task before it asks), and up to five plain
  statements, such as "Nothing goes to a candidate without Priya's OK".
- **First plan**: up to eight tasks, each a deliverable someone could review, each done by Afi or a person. You can
  rename, remove or add tasks.
- **Files**: the Brief, a document Afi writes from the card, and a chat you started it from, when there is one.

Nothing is set up while the card is showing. The card waits until you accept or dismiss it, also if you leave the
workstream, close the window or come back the next day.

## Accept — what Accept does, can I undo setting up a workstream

**Accept** turns the card into "Setting up <name>" with **Undo** for five seconds. Nothing is written in that time:
Undo puts the card back as it was, and leaving the workstream or closing the window then leaves the card waiting.
When the five seconds run out the whole card is applied at once: the name, goal and rules; the people and teams; each
rule line as a decision; the Brief in Files; the tasks, together as the group "First plan" with what waits on what;
and last, in an organization, the workstream handed to who Decides. A toast then says "<name> is set up". After that
there is no undo of the whole: change the parts in People and rules, on a task or in Files. A task cannot be deleted,
only stopped. If a part cannot be applied, the card stays, names it ("Couldn't set up the Brief · …"), and **Accept
the rest** does only what is left.

**Ask Afi to change it** puts "Change the setup: " in Ask Afi for you to finish; Afi answers with a new card that
replaces this one. **Dismiss** drops the card and changes nothing.

## Set it up yourself — creating a workstream without Afi, the manual way, no model

Under Afi's first message, **Set it up yourself** draws the same card empty, headed "Set it up yourself" (Incoming
work: you, Start without asking: never, no tasks, the Brief unticked), without asking the model anything. Accept waits
until it has a name. Fill in what you want and press Accept; Accept and Undo work the same way.

## making a workstream from a chat — Make workstream, set it up from the chat

Make workstream in a chat on your own computer makes the workstream at once, named after the chat, with that chat
brought along, and Afi's first message becomes "I brought your chat <name> along. Want me to set this workstream up
from it?" with **Set it up from the chat** and **Set it up yourself**. Afi then reads the chat (ws_chats) and proposes the card. In an organization a chat stays on the
computer it was made on, so that button is absent there and Afi asks its usual question. Making a workstream from a
document is not there yet.

## the ws_propose_setup tool — how Afi proposes a workstream's setup

Afi proposes with **ws_propose_setup**: a name, a goal, an optional brief, people and teams by name, who owns incoming
work, who decides, a fallback, how much an agent may start without asking, up to five rules, and one to eight tasks
(title, request, done by Afi or a person, what it waits on, a due date). The tool keeps the card for the person who
asked and writes nothing else; only that person sees it. A task's own run cannot propose a setup, and in an
organization the tool answers with any names nobody there answers to, and who is there.
