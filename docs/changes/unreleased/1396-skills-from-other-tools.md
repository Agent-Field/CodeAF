---
kind: changed
title: Claude Code plugin and Codex skills reach the chat, memory off included
pr: 1396
surface: [chat, engine, remote]
invalidates:
  - "With memory.enabled off, the chat had no skill shelf and no use_skill, and the catalog said so in one line naming the setting (#1380). Memory off no longer turns skills off: the shelf is built from the skill folders for that process and thrown away when it ends, use_skill is on the belt, and the switched-off line is gone."
  - "Claude Code plugin skills were never read, because they live in each plugin's own install folder and not in ~/.claude/skills. The skills of every installed and enabled plugin are now read, only those the plugin names in its manifest or marketplace entry, and only for the project a project-scoped plugin was installed in."
  - "Codex's bundled skills in ~/.codex/skills/.system were not read. They are now, ranked below every hand-kept skill and every plugin skill."
  - "A skill folder that is a link to a folder was passed over. It is read now, which is how installers that keep one copy and link it into every tool's folder reach codeaf."
  - "The skill catalog in the system prompt listed at most fifty skills, ordered by recent use, and a skill reached a message only when the message shared words with its description. The catalog now lists every skill in a stable order with its description clipped to 160 characters, up to 12 KiB, then the remaining names up to 2 KiB, and the model opens the one that fits with use_skill. The per-message word match still runs as a first pass."
  - "The /skill picker read skill folders from disk while the conversation read the shelf, so a row could look attachable and not be. The picker now reads the conversation's own shelf, and on the default launch through the local session host the attach, detach and list calls cross the host connection instead of being missing from it."
  - "An attached skill might be taken to ride every message while it is on. A message that carries a picture carries no skills at all, the attached ones included, and draws no skills line; the attachment stays on and the next message without a picture carries it again."
  - "The skill shelf held at most 100 skills. It holds 400."
  - "The manual said a dim `skills carried:` line sits under the message. On the chat surface the line reads `skills · <names>`, and once the answer landed the `▸ worked` chip swallowed it, where even an opened chip did not show it. It now stays under the message with the chip below it; only the headless --once door prints `skills carried:`."
---
A person asked their own codeaf to use a skill and it used none. Three things
stood in the way, each correct from the inside. Memory was off, and the shelf
lived in the memory store, so there was no shelf. Most of their Claude Code
skills arrived inside plugins, which unpack into folders the scan never looked
at. And the skills that were found reached a message only when its words
matched a description, which a request in the person's own words rarely does.

Memory off promises that nothing about the person is carried between
conversations. Skill folders on disk are not about the person, so the shelf is
now built from them either way; with memory off it lives in a temporary store
the process removes on close, and the folders stay the one source of truth.

Plugins are read the way Claude Code decides what is live: installed in
installed_plugins.json, enabled in the layered enabledPlugins settings, and only
the skill folders the plugin names. A plugin skill keeps its bare folder name
and never outranks a skill placed by hand.

The catalog is now the model's menu, the shape Claude Code uses: every skill's
name and purpose in the stable prefix, and the body fetched on demand.
