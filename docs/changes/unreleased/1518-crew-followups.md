---
kind: fixed
title: crew follow-ups — seat-kept checker ceiling, route pins beside the tier row, landing line at the end
pr: 1518
surface: [chat, engine, docs]
invalidates:
  - "The checker's spend ceiling was keyed by model and dropped whenever another seat used the same model, so a fresh profile's one-model narrow fix had none. Spend is now attributed by seat (session.SeatCompleter, marked by run.CrewFactory), and the ceiling holds whatever the other seats run."
  - "At the crew's daily cap, a call to a model the catalog could not price was sent anyway. A call whose price is unknown is now refused at the cap, helpers included. A known price of nothing (a free pool, a local model, a subscription plan, via config.CrewCallPriceAt) is never stopped by a dollar line."
  - "codeaf do's crew line drew a pinned seat as a pushpin emoji, and config.PinMark held it. The line now says `checker kimi-k3 (pinned)`, and PinMark is gone."
  - "A `model@provider` pin was stored inside the tier row (models.tiers.worker|mastermind|high). The row now holds the model alone, and the route lives in models.crew.route.<seat>. MigrateCrew splits old rows silently, once."
  - "A task's landing crew line (`$… (est …) · not right? /redo stronger`) rewrote the start line in place, far up the thread. It now moves to the end of the thread, beside the landing, still one line per task."
  - "/crew's cap row said `per task $5 · daily none` beside the first-run screen's `Daily limit $500`. It now says `crew daily cap`, and names the daily limit on everything codeaf spends under it."
  - "On the run road, a model named in the ask and the `task model` row never reached the router, and the crew's routed worker ran instead. Both now seat the worker as a one-task pin. Blank, the row reads `the crew's worker`, not `follows the conversation`."
  - "/crew's model list drew prices as bare `$0.15/$0.50`. It now uses the model picker's `$0.15/$0.5 per M`."
---

Follow-ups to the Pareto crew (#1436). The manual answers three new questions: how to change a task's worker, whether code goes anywhere that logs it (including the crew's move onto free routes when every paid route is out of reach), and which model is used and what a task costs.
