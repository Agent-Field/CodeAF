---
kind: added
title: A Factory job can wait on other jobs' merges before it starts
pr: 2418
surface: [factory, app]
invalidates: []
---
`factory.waits_set` sets the jobs one Backlog job waits on. It becomes startable
once every parent's pull request has merged; a parent that fails or closes
unmerged blocks it with the reason until it is fixed. A person's Start still
starts it at any time. `factory.features` says whether a factory serves this.
