---
kind: added
title: A Factory group policy starts waiting jobs inside an allowance and stops past it
pr: 2423
surface: [factory, app]
invalidates: []
---
`factory.policy_set` gives a group of jobs allowances (jobs, a dollar cap, an
end date) and a stopped flag. A released waiting job starts on its own only
inside an allowance with room for its budget; a run the policy started holds
when its allowance ends or is spent, and every run of a stopped group holds.
A person's Start or Continue still runs a job as theirs, unless its group is
stopped. `factory.policy_get` reports what each allowance has spent.
