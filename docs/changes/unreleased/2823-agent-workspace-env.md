---
kind: fixed
title: Commands carry the agent's folder as CODEAF_AGENT_WORKSPACE, so Claude canvas writes land
pr: 2823
surface: [engine, chat, docs]
invalidates:
  - "A command's only identity variable was CODEAF_AGENT_DID, and a tool had to read the agent's folder from the command's current directory. Commands, task workers and Claude Code sessions now also carry CODEAF_AGENT_WORKSPACE: the conversation's folder exactly as the app's agent list spells `workspace`, wherever the command runs."
---

The desktop adds a canvas's agent as a member by that folder before it has
seen the agent's DID. Claude models run `cd /tmp` before their `canvas`
commands, so the CLI saw `/tmp` and refused every write `not_member`.
