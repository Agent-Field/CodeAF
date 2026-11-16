---
kind: changed
title: Background calls — titles, reflex, router, page AI, the Library — run on the chosen model
pr: 2696
surface: [chat, app, engine, factory]
invalidates:
  - "The reflex row shipped pointed at google/gemini-2.5-flash and the small-work row at deepseek/deepseek-v4-flash-0731; both now ship empty and follow the chosen model (config.DefaultReflexModel and config.DefaultLowModel no longer exist)."
  - "On a low account with free routes on, the helper rows read nvidia/nemotron-3.5-lightning:free and thinkingmachines/inkling-small:free; that table is gone and the helpers follow the conversation, which is itself on the free rung."
  - "The page AI (insight and its overrides) defaulted to deepseek/deepseek-v4.1-flash on OpenRouter (config.InsightDefault); it now has no engine default and follows a written small-work row, then the chosen model. The task models' insight `default` is always absent."
  - "The Library compiler defaulted to deepseek/deepseek-v4.1-flash (config.DefaultKnowledgeModel); knowledge.model is now an override, and empty means the chosen model. config.KnowledgeModelAt takes the chosen model as a second argument; KnowledgeModelRowAt is the row alone."
  - "Settings' knowledge_model was omitted when empty; it is now always sent, empty meaning it follows the chosen model."
  - "routerconfirm (a mastermind-tier role) rode the crew router's standing planner pick when unpinned; a background role now never reads the worker, checker or planner rows (roles.Background)."
  - "The quorum verifier asked deepseek/deepseek-v4-flash by name; it now resolves the checker seat (roles.RoleAuditor) through the roles ladder."
---

The workstreams-v5 live run (F-20) found a person who picked one model billed
for four: the title, router and reply suggestions on deepseek-v4-flash-0731, the
reflex on gemini-2.5-flash and the router's confirm on the crew router's pick.
The owner's rule is now enforced in internal/roles: every role is classified as
crew work, background or media, a background role reads only its pin and the
reflex or small-work rows a person wrote, and a go/ast law fails any model-id
literal written outside the files where an id is data.
