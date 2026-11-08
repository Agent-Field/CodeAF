# Claude Code as a provider — conversations, crew members and small jobs on your Claude plan

## Claude Code as a provider — use my Claude plan, no API key, connect Claude Code

Settings › Models & providers lists **Claude Code** beside OpenRouter and ChatGPT. It asks for no key: Check finds the `claude` program and its sign-in (it says, for example, `Found Claude Code 2.1.319 · signed in · Claude Max plan`), and Connect adds three models — `claude-code/opus`, `claude-code/sonnet` and `claude-code/haiku`.

Connected, the row reads `Signed in · Claude Max plan · no API key`; the plan is shown only when Claude Code reports it. Not installed, it reads `Not installed on this computer` and offers Check again, not Connect. First run leads with a Claude Code card only when Claude Code is installed and signed in.

Nothing is billed per call on a Claude plan, so these models show `on your Claude plan` instead of a price, and they add nothing to codeaf's spending.

## How a conversation on Claude Code works — Claude Code does the work with its own tools

A conversation or crew member on a Claude Code model is a live Claude Code session working in that conversation's folder, held open between turns. Your message goes in; Claude Code reads, edits and runs commands with its own tools; each tool call shows in the thinking line as `▸ Bash …` or `▸ Edit …` while it runs, and its last message is the reply. A long command says `still …` every 20 seconds.

## Claude Code's edits and commands in the conversation — rows, saved, seen by the next model

When one of Claude Code's own steps finishes, codeaf writes it down in its own format: Bash becomes a `bash` row, Read `read`, Edit and MultiEdit `edit`, Write `write`, Grep `grep`, Glob `find`, LS `ls`, WebFetch `web_fetch`, WebSearch `web_search` — each with its result, as if codeaf had run it. They are saved in the conversation, drawn again when it is reopened, and read by any model the conversation moves to next. Nothing is run twice. A result longer than 16 KB is kept as its start and end. Steps with no codeaf counterpart — a sub-agent, a to-do list, loading a skill — show only while they run.

Edits are accepted without asking. Pushing, opening or merging a pull request, releases and publishing are refused even when your own Claude Code hooks would allow them.

## What Claude Code can do in a conversation — codeaf tools, propose_task, team tools, manual, pictures

It keeps its own tools (Bash, Read, Edit, search) and is also handed codeaf's, as `mcp__codeaf__propose_task`, `mcp__codeaf__manual`, the team tools and the rest of the conversation's belt, less the ones it has its own of (`bash`, `read`, `write`, `edit`, `grep`, `find`, `ls`, `web_fetch`, `web_search`, `view_image`). A call to one of them runs in codeaf exactly as any model's would, with its own row on the page, and Claude Code waits for the result. So it can propose a task, start or message crew members, and read this manual. Pictures attached to your message are passed to it.

## Canvas and skills on Claude Code — make a canvas, codeaf:workaf-canvas

The skills you keep for codeaf and other programs (`.codeaf/skills`, `.agents/skills`, Codex's, Cursor's) are handed to Claude Code as a plugin, so it lists them as `codeaf:<name>` beside its own and loads one when it applies. Skills already in a `.claude/skills` folder or a Claude Code plugin are not handed over twice. So in the desktop app, where the `canvas` command and the `workaf-canvas` skill are installed, Claude Code makes and edits canvases ("mock up a landing page", "make a deck") exactly as other models do, and each canvas is recorded as this conversation's, because its commands carry the conversation's identity (`CODEAF_AGENT_DID`). The skill list is read when its session starts.

## What Claude Code cannot do in a conversation — approvals, effort, new tools mid-session

- **It does not ask before editing.** Edits and commands are accepted; only pushing, pull requests, releases and publishing are refused.
- **The effort setting does not reach it.** Claude Code thinks as its own settings say.
- **Its tool list is fixed when its session starts.** A codeaf tool that becomes available later in the conversation reaches it after codeaf restarts or the conversation moves off Claude Code and back.
- **codeaf keeps no copy of the files it changed.** Its steps are rows in the conversation, but `/rewind` over them takes back the conversation, not Claude Code's edits; use git to put files back.
- It reads Claude Code's own settings, hooks and MCP servers from your machine, and codeaf's skills as above, not codeaf's memory.

## Talking to Claude Code mid-turn — send a message while it works

Typing while Claude Code works does not stop it. The words are written into the running turn, the way T3 Code does it, and Claude Code takes them in at its next step, so a build or an edit in progress finishes. Pressing stop with nothing typed interrupts Claude Code a few seconds later, between tool calls.

## Switching models in a conversation that used Claude Code — and after a restart

Move a conversation onto Claude Code and its first turn is given what was said so far. Move it off and back, and Claude Code is given what was said while it was away. Claude Code's own session id is kept in the conversation's folder, so after codeaf restarts the same session resumes rather than starting over.

## Small jobs with no OpenRouter key — titles, summaries, naming on Claude Code

With only Claude Code connected, the small jobs — naming a conversation, titling a task, summaries — run as one-shot Claude Code calls with no tools, about three seconds each. Connecting Claude Code as the first provider puts new conversations on `claude-code/sonnet`.

## Auto and Claude Code models — why Auto never picks them

Auto never picks a Claude Code model for a task role; Task models says `Auto never picks these · pin one to a role · edits without asking`. Pin one: a Worker pinned to Claude Code hands each task to the `claude` program.

## Claude Code on another computer — the relay, --host

Claude Code runs where codeaf's engine runs. A desktop reaching an engine on another computer through the relay, or the terminal with `--host`, sees and uses that computer's Claude Code, signed in there; install and sign it in on that computer.
