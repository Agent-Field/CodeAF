---
kind: fixed
title: Removing a person ends their place and grants on every join door; Railway script fits the CLI
pr: 2944
surface: [factory, docs, build]
invalidates:
  - "Removing a person who had joined by an anonymous access link left them in People with every grant they held, and a new link brought those grants back. Removal now takes them out of People and ends every grant they hold, on every join door, and a new link brings none of it back. The one thing unchanged is that the same still-open link admits them again as a new member with no grants."
  - "Deleting the last chat of a removed person left their empty folder on the chats' host. The folder is now removed with the chat, only when nothing else is in it; folders left by earlier removals stay."
  - "deploy/org/railway/make-template.sh failed on Railway CLI 5.71.0, because volume add no longer takes --service and the environment edit waited at a prompt without --message. It now adds each volume by the service's ID after the service exists and passes --message on all three edits; an older CLI still gets the older form."
  - "The operator docs did not say that Railway volumes are 50 GB with no size flag, that templates create makes a draft, that a hosted chat's sandbox has no pip, pytest, go, node, make or gcc, or that SWE-AF, PR-AF and the runner call the model provider directly, outside the organization's spend table and limits. They now say each of these, and give a backup recipe for Railway."
---

Grants end by person, in the same removal that ends their chats. Links, invites and the password door admit people as before.
