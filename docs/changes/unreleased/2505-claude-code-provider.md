---
pr: 2505
kind: added
title: Claude Code as a provider, a task program and a crew seat
surface: [engine, app, chat]
invalidates:
  - "The manual said Claude was a worker and never a model: codeaf could not send a request on a Claude plan. Now the person's own signed-in Claude Code is a provider (claude-code, kind agent, no key): a conversation or crew member on claude-code/opus|sonnet|haiku is a live Claude Code session in its folder, small toolless jobs are one-shot `claude -p` calls, and a Worker pinned to it hands tasks to the `claude` program."
  - "A message typed into a running turn cut the model request; on Claude Code that would interrupt its tool call. The transport now leaves a cut turn running for three seconds and writes the correction into it, as T3 Code does; only a stop with nothing following interrupts."
  - "providers.list kinds were key, browser and address. A fourth, agent, means a local program on its own sign-in: keys.check reports the version and plan, keys.set connects with no key, and a key sent for it is refused."
  - "A conversation on Claude Code was a leaf: it had only its own tools and could not propose a task, reach crew members or read the manual. codeaf's belt is now served to it as an in-process MCP server (mcp__codeaf__*, less the tools it has its own of), each call runs in codeaf's turn loop with its own row, and pictures attached to a message are passed as image blocks."
---
