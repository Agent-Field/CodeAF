# claude — hand a task to your own Claude Code

## What /claude is — use Claude Code, use my Claude subscription, Claude plan, Claude as an agent

`claude` is a program codeaf carries that hands a whole task to the Claude Code you installed and signed in to yourself. codeaf starts `claude` in the task's folder, Claude Code plans and edits with its own tools, and codeaf shows each tool call as a step and its last message as the answer.

**Claude is a worker here, not a model.** codeaf cannot send Claude requests on your Claude plan the way it sends them to OpenRouter or a ChatGPT plan, and it holds no Claude credential. Claude Code runs on whatever account you signed it in to. Anthropic's terms for that account apply to the work.

## How do I use Claude Code from codeaf — /claude, codeaf claude, via claude

```
/claude fix the flaky retry test in internal/provider
codeaf claude add a --json flag to the status command
```

Asking in words works too: "have Claude Code do this" makes the chat propose the task with `via: "claude"`. When your crew's working seat is a Claude model, Claude Code runs that family (`opus`, `sonnet` or `haiku`); any other model leaves Claude Code on its own default.

## Where the work lands and what it costs — branch, spending, ceiling

It edits files like senior-dev does: on a branch codeaf cut for the run, with whatever it left uncommitted committed there when it ends. A run starts under up to $10.00 and two hours unless the conversation set less. The cost ceiling is passed to Claude Code as `--max-budget-usd`; the hours are a hard stop.

On a Claude plan, nothing is added to codeaf's spending, because the plan is not billed per call. Signed in with an API key instead, Claude Code's own figure is added.

## Why /claude says Claude Code is not installed — CODEAF_CLAUDE_BIN, not found

The run ends at once with `Claude Code is not installed here: install it, run `claude` once to sign in, or set CODEAF_CLAUDE_BIN to its path` when no `claude` program is on PATH. A `claude` that is only a shell alias or function does not count, because codeaf starts a program, not a shell. Set `CODEAF_CLAUDE_BIN` to the real file.

## What /claude cannot do — questions, permissions, messages

It cannot ask you anything while it works: it runs unattended, with edits and shell commands allowed and every other permission prompt refused. It does not read messages sent to its task while it runs. It does not use codeaf's models, skills or memory; it uses Claude Code's own.
