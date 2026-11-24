---
pr: 2746
kind: changed
title: A Personal factory runs its jobs without GitHub
surface: [engine, app, factory]
invalidates:
  - "The runner on this computer started only once GitHub was connected: a worker attach with no handle was refused, factory.local_run answered \"connect GitHub before turning the runner on\", and a folder job made before GitHub waited in the queue forever. A Personal (solo) factory's runner on this machine (loopback only) now attaches under factory.LocalRunnerHandle, \"this-computer\", while no GitHub token exists, and takes unassigned jobs, so a folder job runs with no GitHub at all. A team factory and any remote peer are still refused a worker with no handle. GitHub is needed only for jobs on GitHub repositories and for Open a pull request."
---
