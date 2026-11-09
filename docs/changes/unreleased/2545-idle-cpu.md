---
kind: fixed
title: An open app with nobody typing no longer keeps several cores busy
pr: 2545
surface: [engine, factory]
invalidates:
  - "Was true: every job chat's person-Stop watch (100 ms, for the chat's whole life) read and decoded every job document on the factory floor twice per look, so ~730 jobs kept `codeaf app` at 200-480% CPU for minutes after launch (2026-11-09). Is true now: the watch remembers its answer against the managed job's own document (os.SameFile, size and modification time, stat'd before reading) — or, when the conversation manages no job, against a shared 100 ms look at the floor's job files rather than the floor folder — and decodes only a job document that changed, sleeping 5 s between looks while idle and waking to 100 ms while work runs."
  - "Was true: every read of the key vault (each Get, Entries, Revisions, IDs) decrypted vault.enc again, and every chat's sync start read it slot by slot twice. Is true now: a vault whose sealed bytes are unchanged since this process last opened them is not decrypted again."
---

`Store.Reads` and `keys.Opened` count decoded factory job documents and vault
decryptions so the idle-cost regressions hold without a clock.
