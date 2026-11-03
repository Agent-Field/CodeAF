---
kind: fixed
title: codeaf job budgets cover all runs
pr: 0
surface: [factory, app]
invalidates:
  - "Job budget stops previously compared only the current run. They now compare all known saved spending on the job."
---

The job budget uses SpentBefore plus the current run's spending. Start,
Carry on, Rerun, Start over and Bring back keep that ceiling. An exhausted
job asks for a raise before paid work starts. Run as new remains a separate
job with zero spending. Older jobs use only their saved known total; money
lost from old saved records is not reconstructed from ledger tags.

The cap question and owned-chat refusal say job total. spending_limit.spent,
spending_limit.next_limit, questions[].spent and questions[].next_limit use
lifetime spending; cost remains this run and lifetime_cost keeps its existing
meaning. A person's continue chooses the next standard ceiling above lifetime
spending, doubling higher ceilings as needed. Auto never grants itself a raise.
A Manual raise above the total resumes work; at or below it work stays held.

A pipeline's default_budget supplies the job ceiling and now covers all runs.
Stage attempt limits and step costs, pipeline intake start.max_cost, the daily
rail and Pareto model selection retain their meanings. Existing cumulative
receipt accounting still retains growth once, including resumed segments,
replayed reports and late usage after a run ends; raising a cap clears no spend.
