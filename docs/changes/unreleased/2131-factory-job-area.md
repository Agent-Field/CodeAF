---
kind: added
title: Each factory job names the area of the repository its change touched
pr: 2131
surface: [engine]
invalidates: []
---

The hello says `factory.job_area`. Jobs on the queue and a job's own read carry
`area`: the repository's declared area most of the job's changed files fall
under, or else the top-level directory most of them sit under, written with a
trailing slash (`cli/`). A job that changed no file, or only files at the root,
carries none. Repositories that declare no areas still send none.
