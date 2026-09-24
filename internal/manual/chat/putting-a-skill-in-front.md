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

## How do I turn a skill off

Open `/skill` and press `enter` on the skill's row, or click the skill's chip
above the message box. The chip carries the skill's name when one is attached
and "N skills" when more than one is, and it stays there for as long as the
attachment is on — including across the messages you send while it is on.
Clicking the chip takes every attached skill off in one gesture, which is the
one way off that does not involve opening the list.
