# use_skill — list or get a skill from the shelf

## What it does

Reads the shelf of active skills this conversation can use: the skills a
person already has for Claude Code, Codex and the other agentskills.io
harnesses, read in place from their folders, and the procedures codeaf saved
after watching them run. `use_skill` has two modes, the way `jobs` and
`settings` do:

- **`list`** — shows every active skill by name with its one-line doc. No internal fields, no paths.
- **`get`** — resolves one name to its path and full doc, which you then `read`. For a skill that arrived as a `SKILL.md` folder the path is that `SKILL.md` file, where it lives; nothing is copied.

## How to use it

```
use_skill mode=list
use_skill mode=get name=linter
```

The name is the skill's folder name, as `list` printed it. A name that
differs only in case still resolves — `release-notes` and `Release-Notes` are
the same skill — and the answer spells the name the shelf holds, so the name you
read back is the one that works next time.

## When a name misses

A name that matches nothing does not dead-end. The answer says how many skills
are active on the shelf and names the nearest handful, so you can re-ask with a
name the shelf actually holds. An empty shelf says so in one plain line.

## Why it exists

A skill whose description shares words with a message is already carried with
it (the dim `skills ·` line) — except a message that carries a picture, which
carries no skills, attached ones included — and the prompt lists every skill on
the shelf with what it is for. `use_skill` is the door onto their bodies: the model
opens the one a request fits, even when the request shares no words with it.

## Can you use skills with memory off

Yes. Skills do not depend on the `notice things to remember` setting (the
`memory.enabled` row): turning it off stops only codeaf keeping lines on its own,
and the shelf lives in the same database memory uses either way. Only when that
database could not be opened at all does codeaf build a shelf of its own at
launch from the skill folders on disk, and throw it away when the
conversation's process ends. The same folder skills are found either way, a
message still carries the skills that suit it, `use_skill` is still on the belt,
and `/skill` still attaches them. The procedures codeaf saved after watching
them run live in that database, so without it only the skills in folders are on
the shelf.

## Why can't you use my skills

Skills work with automatic memory saving off: `memory.enabled` does not disable
them. If a skill is missing, open `/skill` and check its row for a warning, or
type `/skill <folder>` to attach it directly. The folder needs a readable
`SKILL.md` with a name and description. A missing file or invalid frontmatter
can keep it off the shelf. After adding or editing a skill in a scanned folder,
open a new conversation so discovery reads it again.

For a Claude Code plugin skill, the plugin must be installed and enabled in
Claude Code; browsing a marketplace is not installation. If two skills share a
name, the higher-precedence copy wins. The page `skills-from-other-tools`
explains those folders and precedence rules.

A message with a picture does not automatically carry skill instructions, even
for an attached skill. Send the picture, then send the instruction in a message
of its own; the model can still open the skill with `use_skill`.

## Where the shelf comes from

Every launch reads the skill folders before the first message is built and
records each skill as an active fact whose path is the ORIGINAL folder. An
edited `SKILL.md` is read again at the next launch, and a deleted folder drops
off the shelf. The page `skills-from-other-tools` lists every folder read and
which copy wins when two share a name.

## Tool name

The verb is `use_skill` on the belt. A belt that does not carry `propose_task` does not carry this verb either (it is gated on the same condition plus a shelf to read).
