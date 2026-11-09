---
kind: added
title: the jobs search bar can ask the AI which jobs a question is about
pr: 2497
surface: [engine, factory]
invalidates:
  - "The factory's job search matched only words in a job's title, request text, repository and number (desktop search's 1.5-second keyword match). The engine now also serves an AI search: `factory.ai_search_estimate` (free), `factory.ai_search` (starts one run), `factory.ai_search_status` (progress as \"Read 40 of 61 · 2 matches so far\" with a never-backward `share` for a bar, and the matches so far, each with a short reason) and `factory.ai_search_stop`, under hello `factory.ai_search`. There is no event; a surface polls status."
  - "A topic view's AI was the desk's own single errand over at most 250 job titles and labels, with no descriptions read. The engine's AI search reads up to 5,000 jobs, title, labels and the start of each description in a screen of 20 at a time, then judges at most 60 with the routing rule's own yes/no question over the whole description."
  - "A routing rule's AI reason was one sentence, and models wrote it as \"The item adds…\". The judge is now asked for a short, direct phrase about the work (\"Retries the payment webhook when the bank times out.\") and told never to open with \"The item\"; routing reasons and AI search reasons both read that way. Reasons kept before the change keep their old wording until the item changes."
  - "An AI cost shown before a run was one figure, \"about $0.01\" or \"under $0.01\", and a run could spend past it. The AI search's estimate is now a range from typical to most (\"about $0.01–$0.02\"), and the run stops itself at twice the most: it keeps the matches so far and says \"stopped at the cost limit · N not read\" (`limited` and `unread` on the status)."
  - "Factory AI on the spend ledger was `factory triage`, `factory scout` and `factory filter`. The AI search adds `factory search`, which counts toward the factory's day the same way."
  - "Asking the same AI question twice cost the same twice. The engine now keeps every AI search answer per factory, keyed by the question and the item's words, so asking again (a saved view's Refresh) asks only about items that arrived or were edited since; on factory-demo a repeat cost $0.00."
---

Saved AI views are not judged against arriving issues by themselves: nothing
runs while nobody is looking, and a routing rule's AI question is the tool for
judging work as it arrives.
