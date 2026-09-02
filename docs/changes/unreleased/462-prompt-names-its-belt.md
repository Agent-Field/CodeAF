---
kind: fixed
title: The system prompt names exactly the tools the agent reading it can call
pr: 462
surface: [chat, engine]
invalidates:
  - "prompts/system.md named `tasks`, `settings`, `change_setting`, `search_conversations`, `watch` and `use_service` to every agent, while the belt gates all six — no longer true. Those sentences are composed at render time from the belt's own predicates, so a worker is only ever told about a verb it actually has."
  - "The render step conditioned only the fan-out page and the divide page on the agent's shape. It now also composes the tool-naming facts of the session-facts section, from Config.hasStore, maySeeSettings, mayWatch, mayProposeTask and hasConnect."
  - "prompts/system.md's `# Session facts` section held those bullets. It no longer does: it carries a `BELT_FACTS` token, and the sentences live in internal/session/beltfacts.go beside the predicates that choose between them."
  - "A worker with a tool missing was told about it anyway and found out by calling it. It is now told what it cannot do from where it stands and what to do instead — where the record of earlier work is unreachable, that a preference is changed at `/settings` and not from a task, that earlier conversations cannot be looked up, and that a foreground `bash` call is how it waits when there is no `watch`."
  - "Config.mayFanOut and Config.mayDivide were the only belt predicates answerable from a config. There are five more, and Agent.mayProposeTask now delegates to Config.mayProposeTask rather than repeating it."
  - "prefixbudget_test.go weighed the `systemPrompt` variable. There is no single page any more, so it weighs the widest one — every tool-naming fact in its present case."
---

The prompt was one embedded text and the belt was not one belt, so five families
of tools were promised to workers that do not carry them. A node on the floor of
its tree did what it was told, called `tasks` first and was answered `Unknown
tool: tasks` — a step spent, and a worker with no way to know which of the rest
of its instructions were also false.

prompt_belt_test.go is the law's new enforcement, both ways over every shape this
package builds: every tool the rendered page names is on that shape's belt, and
every tool that varies between belts and is named in the page has a fragment
composed from its predicate or a line in a debt ledger. The remaining debt is
written down rather than merely absent — the `## Work or words` section still
names `propose_task`, `fork` and the harness verbs for everybody.
