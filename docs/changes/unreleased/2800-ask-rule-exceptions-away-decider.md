---
kind: added
title: Ask-rule exceptions Afi suggests, a fallback that answers after a set wait, unjoined invites
pr: 2800
surface: [factory, app]
invalidates:
  - "The ask rule was one switch (asks before publishing or sending, or not at all). It now has three settings in rules.asks_before: outward (the default, unchanged), shared_file and every_task; publishes_without_asking still turns it off."
  - "An agent's Ask had no kind and nothing could stop one being raised. Asks now carry kind, and a workstream's rules.exceptions lists kinds Afi decides itself: the agent door records the recommended answer under Assumed instead of raising the Ask."
  - "Nothing suggested changing the rules. With more than 10 Asks waiting and at least 3 of one kind, GET /v1/workstreams/{id} carries rule_suggestion for the steward only; Yes is a rules write adding the exception, Not now writes rules.not_now and rests it 7 days."
  - "The fallback person ('If unanswered for a day') could answer after exactly 24 hours, but nothing showed them the Ask. The wait is now rules.fallback_after_hours (12, 24, 48 or 72; 0 is 24), and every overdue Ask carries covered_by at read time in tasks, a task, /v1/work and the overview, so the fallback's Inbox can list it."
  - "A task given to someone invited waited forever if they never joined. After 3 days the server puts an Ask 'Hand to someone else?' on it for its owner (the steward when the owner is the invitee): Hand it to me, Give it to Afi, Keep waiting (asks again 3 days later)."
  - "Removing someone from a workstream handed their open tasks to the fallback, else the steward, and the thread said the handle ('Handed to mia'). They now go to whoever removed them (the fallback, else the steward, only when someone leaves by themselves), and the thread says the display name."
  - "appwire's OrgWorkstreamRules lacked publishes_without_asking and factory_since, so every rules save from the app reset the ask rule to on. It now carries every field the server keeps, and the server keeps exceptions, not_now, asks_before and the wait when a writer omits them. OrgMember now carries invited."
---

AnswerTask, MayAnswer and MayAnswerAsk take a factory.Standin (the fallback and
its wait) instead of a bare handle.
