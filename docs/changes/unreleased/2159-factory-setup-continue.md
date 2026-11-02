---
kind: added
title: Factory setup can continue before every repository is read, check a checkout folder, and quote repository sizes
pr: 2159
surface: [engine]
invalidates:
  - "Was true: setup left reading only after every chosen repository was read. Is true now: an engine whose hello says `factory.setup_continue` takes `factory.setup_continue {caller}` (the factory's setup verb `continue`) in reading once one chosen repository is read whole; repositories still being read say `skipped`, keep the issues they brought held in Backlog, and are not watched."
  - "Was true: `clone_mb` was always 0. Is true now: it is the size GitHub's repository list quotes, in megabytes rounded up."
  - "Was true: a checkout folder was checked only for an absolute path. Is true now: an engine whose hello says `factory.setup_folder_check` answers `factory.setup_folder_check {caller, folder}` with `{folder, exists, checked, writable, free_bytes}` measured at the nearest existing folder, creating nothing."
---
