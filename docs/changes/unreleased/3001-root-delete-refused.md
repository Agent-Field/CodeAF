---
kind: changed
title: Deleting the root, the home folder or a system folder is refused outright, with no Approve
pr: 3001
surface: [chat, engine, docs]
invalidates:
  - "A recursive remove aimed at `/`, `~`, `$HOME` or an absolute path was a critical command that asked first: a person could press Approve, and under `--yolo` or any allow-all setting it still asked. Now a recursive remove of `/`, `/*`, `~`, `$HOME`, the person's home folder or a top-level system folder (`/home`, `/Users`, `/root`, `/etc`, `/usr`, `/var`, `/bin`, `/sbin`, `/lib`, `/lib64`, `/opt`, `/boot`, `/System`, `/Library`, `/Applications`), `rm --no-preserve-root`, and `find` of one of those with `-delete` are refused with no question, in every approval mode, for the model, for a task and for the person's own `!` command."
  - "Every other absolute path such as `rm -rf /var/tmp/x` still asks, and a relative `rm -rf build` still does not; the other critical shapes (`mkfs`, `dd` onto a device, a redirect onto a raw disk, shutdown, the fork bomb) are unchanged and still ask."
---

The refusal names the command, says codeaf never runs it and that there is no
Approve, tells the model not to try another route, and says the person can run
it in their own terminal. The headless harness's `sh` tool gets the same refusal.
