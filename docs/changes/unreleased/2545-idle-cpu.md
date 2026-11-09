---
kind: fixed
title: An open app with nobody typing no longer keeps several cores busy
pr: 2545
surface: [engine, factory]
invalidates:
  - "Was true: every job chat's person-Stop watch (100 ms, for the chat's whole life) read and decoded every job document on the factory floor twice per look, so ~730 jobs kept `codeaf app` at 200-480% CPU for minutes after launch (2026-11-09). Is true now: the watch answers from a per-floor manager index rebuilt only when the floor folder changes, and re-reads the managed job only when its document is replaced; an idle look is two stat calls. The tick and the Stop bound (one tick plus one read) are unchanged."
  - "Was true: every read of the key vault (each Get, Entries, Revisions, IDs) decrypted vault.enc again, and every chat's sync start read it slot by slot twice. Is true now: a vault whose sealed bytes are unchanged since this process last opened them is not decrypted again."
---

Factory HTTP requests (`settleJobs`) and worker pulls (`lease.Desk.Pull`) still
list the whole floor once per request; their cost follows polling rate.
