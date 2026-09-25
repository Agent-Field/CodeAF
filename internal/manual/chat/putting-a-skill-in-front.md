# Putting a skill in front of this conversation

## How do I use a skill for this

Type `/skill` and a space. The shelf opens under your message box: every skill
this project and this machine hold, the ones already attached at the top. Type
to narrow the list, move with the arrows, and press `enter` on a skill to turn
it on. Enter does not close the list — three skills are three presses of it.
Press `enter` on a skill that is already on to turn it off. `esc` closes the
list and leaves your message exactly as you typed it.

`/skills` is the same command.

A skill that is on is marked with a filled dot on its row; one that is off
carries a hollow one. Each row says what the skill is for and where it came
from — this project, your home directory, or the shelf codeaf keeps for you.

The list holds the skills you installed for Claude Code, Codex and the other
agentskills.io tools, read where they live — the page
`skills-from-other-tools` says which folders. It works the same with memory
on or off, and in the ordinary launch, where the conversation runs in this
workspace's session host.

## Why a skill row says it cannot be attached

A row ending `this conversation has no skill shelf, so this cannot be attached`
is a skill found on disk in a conversation with no shelf to resolve it
against, so turning it on would do nothing. It happens when the shelf could not
be built at launch, or when the conversation runs in a session host from an
older codeaf; relaunching on the current one fixes both. When the whole
conversation cannot carry attached skills, choosing a row says
`this conversation cannot carry attached skills` instead.

## Attach a skill from any folder

Type `/skill` followed by a path — `/skill ~/notes/my-skill`, `/skill
./tools/reviewer` — and the list offers one extra row at the bottom: attach the
skill in that folder. Press `enter` on it and the skill in that folder is put
in front of this conversation, read where it lives; nothing is copied
anywhere. A folder with no `SKILL.md` is refused in one line that says what
was missing.

## Why a message with a picture carried no skills — not even the ones I attached

**A message that carries a picture carries no skills at all.** That is a message
with an `[image #1]` token in it, from `/attach`, a drop or a paste. It carries
neither the skills codeaf would have chosen for its words nor the ones you
turned on with `/skill`. No `skills ·` line is drawn under it, and
`codeaf chat --once` prints no `skills carried:` line for it.

Nothing is taken off. An attached skill's chip stays above the message box, and
your next message without a picture carries the skill again. The model still
has the shelf during a picture turn: the skill list in its prompt and
`use_skill` both work, so it can open a skill by itself.

When a skill has to shape the answer about a picture, send the picture, then say
what you want in a message of its own. That message carries the attached skills.

## How do I turn a skill off

Open `/skill` and press `enter` on the skill's row, or click the skill's chip
above the message box. The chip carries the skill's name when one is attached
and "N skills" when more than one is, and it stays there for as long as the
attachment is on — including across the messages you send while it is on.
Clicking the chip takes every attached skill off in one gesture, which is the
one way off that does not involve opening the list.
