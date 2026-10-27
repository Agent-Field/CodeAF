---
kind: fixed
title: Starting a job runs the pipeline its repository's file says now
pr: 2003
surface: [engine, docs]
invalidates:
  - "Was true: an issue was stamped with the default steps when the poll first saw it, and only an automatic start read the repository's `.codeaf/factory.yaml` again, so a person's Start (and the manager's `factory_start`) ran the default steps even after the repository's pipeline was edited. Is true now: every start of a job that has never run reads its repository's pipeline and runs the steps it names for the job's kind. Only a job still on the default steps it arrived with is changed: one whose steps were changed for it, one that has run, and one made with steps of its own keep theirs; with the repository's file unreadable the job keeps the steps it has, and with the factory's own rules unreadable it does not start."
  - "Was true: a repository's pipeline read was kept until the next automatic-start pass, so a factory with Pick up issues off never saw a pipeline merged on GitHub until it restarted. Is true now: a read older than 15 seconds is asked again, by its ETag, so an unchanged file costs one not-modified response."
---
