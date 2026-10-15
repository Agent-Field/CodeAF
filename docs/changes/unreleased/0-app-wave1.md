---
kind: added
title: codeaf app starts agents from a first message, reads transcripts, retries, and saves always rules
pr: 0
surface: [engine]
invalidates:
  - "codeaf app's agents.create took only a workspace and a model. It now takes {workspace?, model?, budget_usd?, text?}: no workspace is the home folder, a budget is the conversation's own spend limit, and words start the first turn at once and name the agent by its first forty characters with an ellipsis."
  - "An app agent was renamed by the engine's generated title after its first turn. A conversation launched with deterministic names keeps the name its first message gave it, on disk as well as on the wire."
  - "A permission ask offered the engine's digit keys. It now offers allow_once, deny and always (no always on a call that cannot be undone), carries subkind permission and irreversible, and an always on a shell call writes that one command line as an allow rule the engine reads before the call goes on."
  - "turn.done said only ok and an error string. It now sorts a failure as provider, budget, stopped or other, with the provider's status and its own words, and agents.retry sends the failed turn's words again."
---

New methods: `agents.entries` (the transcript off disk, oldest first, paged
with `before`/`limit`, for agents nothing holds), `agents.set_model`,
`agents.retry`, `models.list` (the remembered list; with none, one lazy fetch
starts behind the answer), `settings.get` and `spend.today`. New events:
`entry.added` (the person's message, each answer, notices, errors and the
receipt after every closed ask, e.g. "Allowed once · you · 14:02") and
`spend.changed` after a turn's spending reaches the ledger. `ask.closed` now
carries the option and the moment.

The engine raises no budget question, so the `budget` subkind is never sent
yet. Transcript entries carry the conversation's last moment rather than their
own, and no thinking, errors or receipts, because the journal keeps none of
those per message; entries said live are named apart from listed ones.
