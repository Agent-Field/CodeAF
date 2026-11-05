---
kind: added
title: Chat folder names sync through the hosted relay
pr: 2431
surface: [remote]
invalidates:
  - "Was true: the hosted relay did not advertise chat folders, so clients withheld folder names. Is true now: the hosted relay advertises and retains sealed folder names between paired computers."
  - "Was true: the release service probe required only conditional takes from its sync service. Is true now: it also requires the chat folders promise before a build ships."
---

Folder names remain opaque to the relay. Creation stores the optional string;
a publish without a name retains the stored name, and a nonempty name replaces
it. Releasing a chat preserves its folder. The existing request-body ceiling
applies without a separate folder-size or ciphertext-format restriction.
