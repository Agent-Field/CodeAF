# claude — hand a task to your own Claude Code

## What /claude is — use Claude Code, use my Claude subscription, Claude plan, Claude as an agent

`claude` is a program codeaf carries that hands a whole task to the Claude Code you installed and signed in to yourself. codeaf starts `claude` in the task's folder, Claude Code plans and edits with its own tools, and codeaf shows each tool call as a step and its last message as the answer.

codeaf holds no Claude credential: Claude Code runs on whatever account you signed it in to, and Anthropic's terms for that account apply to the work. To put a whole conversation or a crew member on Claude Code rather than one task, connect Claude Code as a provider — see "Claude Code as a provider".

## How do I use Claude Code from codeaf — /claude, codeaf claude, via claude

```
/claude fix the flaky retry test in internal/provider
codeaf claude add a --json flag to the status command
```

Asking in words works too: "have Claude Code do this" makes the chat propose the task with `via: "claude"`. When your crew's working seat is a Claude model, Claude Code runs that family (`opus`, `sonnet` or `haiku`); any other model leaves Claude Code on its own default.

With Claude Code connected as a provider, pinning the Worker role to a Claude Code model in Task models sends every task to `claude` without naming it: the task is handed to Claude Code, not run by codeaf's own loop.

## Where the work lands and what it costs — branch, spending, ceiling

It edits files like senior-dev does: on a branch codeaf cut for the run, with whatever it left uncommitted committed there when it ends. A run starts under up to $10.00 and two hours unless the conversation set less. The cost ceiling is passed to Claude Code as `--max-budget-usd`; the hours are a hard stop.

On a Claude plan, nothing is added to codeaf's spending, because the plan is not billed per call. Signed in with an API key instead, Claude Code's own figure is added.

## Why /claude says Claude Code is not installed — CODEAF_CLAUDE_BIN, not found

The run ends at once with `Claude Code is not installed here: install it, run `claude` once to sign in, or set CODEAF_CLAUDE_BIN to its path` when no `claude` program is on PATH. A `claude` that is only a shell alias or function does not count, because codeaf starts a program, not a shell. Set `CODEAF_CLAUDE_BIN` to the real file.

## Talking to Claude Code while it works — send a message, change direction, stop it

Words you type on its task's page reach Claude Code while it works, the same as they reach senior-dev. Claude Code takes them into the turn it is on or starts a new turn for them, and the task ends only once it has answered everything it was sent and gone quiet for a few seconds. Stopping the task interrupts Claude Code's turn first, so it stops between tool calls rather than in the middle of writing a file.

Each run keeps Claude Code's own session id, so `codeaf claude run --resume <id> -- <brief>` carries on that same conversation.

## What /claude refuses and cannot do — git push, pull requests, publish, questions

It may not push, open or merge a pull request, cut a release, or publish a package. Those commands are refused with `Pushing, publishing and opening pull requests are the person's to do; leave the work on this branch.`, and the refusal shows as a step. The rule holds even when your own Claude Code hooks would allow the command. Everything else inside the run's branch is allowed without asking.

It cannot ask you a question and wait: nobody is assumed to be watching. It does not use codeaf's models, skills or memory; it uses Claude Code's own settings, hooks and MCP servers from your machine.
