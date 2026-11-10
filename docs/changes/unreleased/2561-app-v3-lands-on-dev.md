---
kind: changed
title: app/v3 lands on dev, and dev is the one trunk again
pr: 2561
surface: [app, engine, chat, build]
invalidates:
  - "Was true: the desktop engine line lived on app/v3, 876 commits ahead of dev, and the desk built its engine from an app/v3 checkout. Is true now: app/v3 is merged into dev, the desk builds its engine from dev, and app/v3 is frozen and then removed. New work branches from dev and opens its pull request against dev."
  - "Was true: a device's own directory record was written directly on dev, so a refused chat startup made one relay call. Is true now: the write reads the directory first (app/v3's rule), so waiting messages (Outbox, Took) and an unchanged record survive, and a refused startup makes two calls, the read and the refused write, once per wait."
  - "Was true: on app/v3 every task git command ran with automatic maintenance switched off. Is true now: maintenance is switched off only in the copies codeaf owns (dev's rule); a person's own repository keeps git's automatic maintenance."
---

Two lines that had diverged for weeks are one again, so a fix lands once and reaches every release. The merge keeps every behaviour from both sides. Where they disagreed, the pull request says which rule won and why.
