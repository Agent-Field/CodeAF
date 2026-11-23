---
kind: changed
title: app/v3 lands on dev, and dev is the one trunk again
pr: 2561
surface: [app, engine, chat, build]
invalidates:
  - "Was true: the desktop engine line lived on app/v3, 876 commits ahead of dev, and the desk built its engine from an app/v3 checkout. Is true now: app/v3 is merged into dev, the desk builds its engine from dev, and app/v3 is frozen and then removed. New work branches from dev and opens its pull request against dev."
  - "Was true: a device's own directory record was written directly on dev, so a refused chat startup made one relay call. Is true now: the write reads the directory first (app/v3's rule), so waiting messages (Outbox, Took) and an unchanged record survive, and a refused startup makes two calls, the read and the refused write, once per wait."
  - "Was true: on app/v3 every task git command ran with automatic maintenance switched off. Is true now: maintenance is switched off only in the copies codeaf owns (dev's rule); a person's own repository keeps git's automatic maintenance."
  - "Was true: app/v3 and dev each had their own rule for recognising a paid call already in the spending ledger, and on app/v3 reopening a chat with no receipts booked its paid turns and folded task spend a second time (once per row, enough to trip the daily limit falsely). Is true now: one rule, session.UsageCallID, decides: a carried call id first, then the eight-field fingerprint, and a line with neither is still counted against its session's earlier rows. Reopening such a chat any number of times leaves the ledger unchanged."
  - "Was true: closing the desktop engine could return while things it had started were still running: the solo factory's GitHub poll, people and CODEOWNERS reads, a brief write already past its wait, or a model-catalog warm-up. A second close returned at once. Is true now: close cancels each of them and waits for it (at most a second for a GitHub read, and the pool's close budget for a brief write), a second close waits for the first, and a factory with no GitHub token reads no profiles at all."
  - "Was true: on dev the usage writer appended to the spending ledger without reading it. Is true now: the first model call of each process reads the ledger once, so the single UsageCallID rule can refuse to book a paid call twice. It costs about 0.6 microseconds per row of history: about 6 ms at 10,000 rows, about 60 ms at 100,000, about 600 ms at a million on a laptop, and more than 2 s on a slow machine. The turn never waits for it. A quit can: on a ledger that large, the 2-second final flush gives up and the last row lands when the writer is joined, and nothing is lost. #2593 tracks taking it off the quit path."
---

Two lines that had diverged for weeks are one again, so a fix lands once and reaches every release. The merge keeps every behaviour from both sides. Where they disagreed, the pull request says which rule won and why.

It lands on `dev` as one squash commit, because `dev` accepts only squash merges. The 906 commits that were reviewed on `app/v3` and on the landing branch stay reachable, and bisectable, at the tag `app-v3-final`.
