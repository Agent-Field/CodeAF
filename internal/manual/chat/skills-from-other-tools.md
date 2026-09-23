# Skills from Claude Code, Codex and other tools

## Can you use my Claude Code and Codex skills

Yes, directly. Any skill you already installed for Claude Code, Codex, Cursor,
Gemini or another agentskills.io tool — a folder holding a `SKILL.md` whose
frontmatter has a `name` and a `description` — is read where it lives. Nothing
is copied or reinstalled. Each launch reads the folders again before the first
message, so a skill you add or edit shows up the next time you open codeaf.

Once found, a skill is used two ways. Automatically: a message whose words
match a skill's description carries that skill with it, and a dim
`skills carried:` line under the message names it. By hand: `/skill` puts one
in front of the conversation until you take it off. `use_skill` lists and
reads them all.

## Which folders are read

Under the project folder and under your home folder, in this order:

- `.codeaf/skills`, `.agents/skills`, `.claude/skills`, `.codex/skills`,
  `.cursor/skills`, `.gemini/skills` — each direct child folder holding a
  `SKILL.md` is one skill. A child that is a link to a folder counts too, which
  is how installers that keep one copy and link it into every tool's folder
  reach codeaf.
- The skills of every Claude Code plugin that is installed and enabled.
- Codex's own bundled skills, in `.codex/skills/.system`.

Nothing deeper is walked: a `SKILL.md` two folders down from a skills folder is
not read.

## Why is my Claude Code plugin skill missing

A plugin's skills are read only when Claude Code itself would load them: the
plugin is listed in `~/.claude/plugins/installed_plugins.json`, and the
`enabledPlugins` setting says `true` for it. That setting is read from
`~/.claude/settings.json`, then the project's `.claude/settings.json`, then
its `.claude/settings.local.json`, each overriding the one before. A plugin
installed for one project is read only in that project.

So a plugin that is switched off, one you only browsed in a marketplace, and an
older cached version of a plugin you updated are all left alone, on purpose.
Turn the plugin on in Claude Code and relaunch codeaf.

## Two skills with the same name

One of them wins, and the other stays listed as shadowed rather than vanishing.
A project skill beats a home one. Within one place, the folders above win in
their order, then plugin skills, then Codex's bundled ones — a skill you placed
by hand always beats one that arrived inside a plugin or with a tool. Between
two plugins, the one whose `name@marketplace` sorts first wins. A plugin skill
is known by its own folder name, not Claude Code's `plugin:skill` spelling.

## Do skills work with memory off

Yes. Turning `memory.enabled` off stops codeaf remembering anything about you
across conversations; it does not take your skills away. codeaf builds the
shelf from the folders for that conversation's process and discards it when
the process ends.

## What is not read

A skill whose `SKILL.md` has no `name`, no `description` or frontmatter that
does not parse is skipped. Claude Code's plugin settings that hide a skill
from the model or from the slash menu are not honoured yet: every skill of an
enabled plugin is offered both ways.
