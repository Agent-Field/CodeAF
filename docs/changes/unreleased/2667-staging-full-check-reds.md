---
kind: fixed
title: A usage writer whose home was removed now stops, and the reds that blocked staging are fixed
pr: 2667
surface: [engine, build]
invalidates:
  - "A usage writer whose home directory had been removed retried its journal sync forever, so closing the process or calling StopUsageWriter returned without joining it. A missing journal now counts as nothing left to sync, and the writer stops and joins."
  - "CODEAF_FACTORY_RUNTIME_DIR was read by the factory runner but not registered. It is now listed as operator plumbing, so it is not a settings row."
---

Promote to staging failed three Fridays running on a red Full check. The reds were registry
and law tests that the carried app/v3 and factory pull requests had outrun, plus order-dependent
tests that left a usage writer running on a temporary directory. The factory serve fallback pickup
and the hosted quit with the relay down are still flaky in CI and not reproduced locally.
