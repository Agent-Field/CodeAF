---
kind: changed
title: Pipeline files preserve runtime step words and hold steps that cannot run safely
pr: 1846
surface: [engine]
invalidates:
  - "Pipeline YAML previously carried only prompt, until and max for each step. It now preserves the runner's existing kind, when, effort and fanout words, while legacy files keep their defaults."
  - "A model-only stop sentence could be appended to a check command or publishing verb. Such steps now hold for a person, with a problem explaining why; the command or verb never receives the sentence."
  - "An unreadable kind or a promptless gate could disappear or become a chat. Unsafe steps now keep their place as person gates, and writers refuse lossy permission-changing conversions before replacing a file."
  - "Structured set_step edits could change execution kinds without a typed app preview. Non-chat steps are now locked for structured editing; the app still exposes only prompt, until and max."
---

Markdown sections that use the same step name with different runtime behaviour retain separate definitions. Explicit chat is canonical with the omitted default. The desktop's additional runtime-field controls remain pending.
