# Claude Code as a provider — conversations, crew members and small jobs on your Claude plan

## Claude Code as a provider — use my Claude plan, no API key, connect Claude Code

Settings › Models & providers lists **Claude Code** beside OpenRouter and ChatGPT. It asks for no key: Check finds the `claude` program and its sign-in (it says, for example, `Found Claude Code 2.1.319 · signed in · Claude Max plan`), and Connect adds three models — `claude-code/opus`, `claude-code/sonnet` and `claude-code/haiku`.

Connected, the row reads `Signed in · Claude Max plan · no API key`; the plan is shown only when Claude Code reports it. Not installed, it reads `Not installed on this computer` and offers Check again, not Connect. First run leads with a Claude Code card only when Claude Code is installed and signed in.

Nothing is billed per call on a Claude plan, so these models show `on your Claude plan` instead of a price, and they add nothing to codeaf's spending.

## How a conversation on Claude Code works — Claude Code does the work with its own tools

A conversation or crew member on a Claude Code model is a live Claude Code session working in that conversation's folder, held open between turns. Your message goes in; Claude Code reads, edits and runs commands with its own tools; each tool call shows in the thinking line as `▸ Bash …` or `▸ Edit …`, and its last message is the reply. A long command says `still …` every 20 seconds.

Edits are accepted without asking. Pushing, opening or merging a pull request, releases and publishing are refused even when your own Claude Code hooks would allow them.

## What Claude Code cannot do in a conversation — codeaf tools, propose_task, team tools, pictures

In that seat Claude Code uses its own tools, not codeaf's: it cannot call `propose_task`, the team tools, `manual`, codeaf's memory or skills, and it reads Claude Code's own settings, hooks and MCP servers instead. Pictures attached to a message are not passed to it; only the words are. A crew member on Claude Code still reports finished to its crew like any member.

## Talking to Claude Code mid-turn — send a message while it works

Typing while Claude Code works does not stop it. The words are written into the running turn, the way T3 Code does it, and Claude Code takes them in at its next step, so a build or an edit in progress finishes. Pressing stop with nothing typed interrupts Claude Code a few seconds later, between tool calls.

## Switching models in a conversation that used Claude Code — and after a restart

Move a conversation onto Claude Code and its first turn is given what was said so far. Move it off and back, and Claude Code is given what was said while it was away. Claude Code's own session id is kept in the conversation's folder, so after codeaf restarts the same session resumes rather than starting over.

## Small jobs with no OpenRouter key — titles, summaries, naming on Claude Code

With only Claude Code connected, the small jobs — naming a conversation, titling a task, summaries — run as one-shot Claude Code calls with no tools, about three seconds each. Connecting Claude Code as the first provider puts new conversations on `claude-code/sonnet`.

## Auto and Claude Code models — why Auto never picks them

Auto never picks a Claude Code model for a task role; Task models says `Auto never picks these · pin one to a role`. Pin one: a Worker pinned to Claude Code hands each task to the `claude` program.

## Claude Code on another computer — the relay, --host

Claude Code runs where codeaf's engine runs. A desktop reaching an engine on another computer through the relay, or the terminal with `--host`, sees and uses that computer's Claude Code, signed in there; install and sign it in on that computer.
