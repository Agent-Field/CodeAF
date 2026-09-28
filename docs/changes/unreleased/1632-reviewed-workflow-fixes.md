---
kind: fixed
title: Home tasks, standing work, and traffic keep their context and navigation
pr: 1632
surface: [chat, engine]
invalidates:
  - "Every codeaf, devaf or stageaf sharing `~/.codeaf` reads every standing order. An isolated order is standing schema 2, and a build from before isolation skips it."
---

Home task drafts bind to the project currently displayed. Standing work respects
project scope, shows its allowance and activity, and hands off an approved
one-time run without accidentally creating a recurring schedule. Explicit branch
isolation preserves unfinished changes in the worker copy, while ordinary
one-time work continues in place. Standing timers have a single owner and
approval receipts reflect whether scheduling succeeded. Empty extracted briefs
are rejected before execution.

Traffic rows show compact ages and open their exact message, including completed
work hidden inside a collapsed group. Reminder labels separate their cadence
from their title. Unknown timestamps remain absent.

Held tasks now notice changed machine limits and retain their accepted request across
engine restarts, reusing the focused recovery fix from #1619. A persistent engine
restart also retires old reply streams, so a follow-up no longer disappears when
its new stream number matches a completed reply from the previous engine.

Held tasks follow changed admission limits and resume with their accepted folder,
crew pins, fallback history, and spending limits after a restart. Records without
complete recovery policy or with unresolved model calls remain interrupted. A
reopened conversation replaces stale connection streams when its owner changes,
so new replies and replay cursors belong to the current session.

New-member Traffic roots now open their accepted start call, including calls
inside collapsed work. New start receipts carry the root message number; older
receipts use only an unambiguous current-team match. Delivered member briefs
remain navigable from the same root.

An isolated standing order is written as standing schema 2, so an older codeaf,
devaf or stageaf sharing the same home skips it instead of firing it in the
person's own checkout; every other order stays at schema 1 and older builds keep
firing it. A Traffic row now opens its own team's message even when the same
conversation sent a `team_send` into another team that carries the same number.

A closed conversation's sweep keeps a landed task copy that gained files, edits
or commits after its cleanup failed, instead of retiring it with that work. A
Traffic reply row opened on the default engine road waits for the member's
conversation to finish arriving before it lands. On the default engine road,
`ctrl+r` in `/model` refreshes every connected provider and a provider with an
empty cached list is fetched once at launch, as it already was under `--no-host`.
