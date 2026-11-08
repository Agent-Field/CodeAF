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
  - "Claude Code's own steps (Bash, Read, Edit, Write, Grep...) were reasoning lines only: codeaf kept just its final answer, so the page had no rows and the next model saw no account of the work. Each finished step is now handed up the stream (provider StreamStepsRan, `codeaf_steps` delta) and recorded as codeaf's own call and result (bash/read/edit/write/grep/find/ls/web_fetch/web_search), drawn, saved, and read by the next model; nothing is run twice."
  - "Claude Code reported its whole turn's summed input as the request's input, so a four-step turn looked like a full 200k window and codeaf compacted a three-line conversation. It now reports what the turn's last call read."
  - "Claude Code could not see codeaf's skills, so it could not make a canvas. Conversations and tasks on it now load the skills kept for codeaf and other programs as a session plugin (`codeaf:<name>`), and its commands carry the conversation's CODEAF_AGENT_DID, so a canvas it makes is the conversation's."
  - "A turn on Claude Code took the LAST user-role message as the person's words, which after the first turn is codeaf's own session note; Claude Code answered the note and called the person's real message something it had not seen. It is now given every user-role message since the last answer, notes included as the notes they say they are."
  - "`codeaf connect claude-code` and the chat's /connect row treated Claude Code as a key service: a piped key saved a bare row with no models, the window fell to 128k and a local chat refused every turn. Both now run the same connect the app does (no key; three models with Claude's 200k window)."
---
