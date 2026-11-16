---
pr: 2661
kind: changed
title: A person joins an organization, never Factory; Settings › Factory is Settings › Organizations
surface: [engine, factory, app, docs]
invalidates:
  - "Was true: the desktop app had a Settings › Factory section, and queued-job notices pointed to Settings → Factory → Your computers. Is true: it is Settings › Organizations, one section per organization with that organization's Factory settings inside it, and the notices point to Settings → Organizations → Your computers."
  - "Was true: people were told to join a factory with a factory access link. Is true: they join an organization with its invite link, and its workstreams, canvases, chats and Factory come with it. The link's spelling, codeaf://factory/join, is unchanged."
---

The manual answers "how do I join an organization" and "where did Settings Factory go".
The CLI refuses a wrong link with "use an organization invite link". No wire name,
method or persisted identifier changed, so old invite links keep working.
