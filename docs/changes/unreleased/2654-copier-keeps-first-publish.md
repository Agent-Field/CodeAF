---
kind: fixed
title: A new chat keeps its hold when the background copier shares it first
pr: 2654
surface: [engine]
invalidates:
  - "Was true: a chat started before its directory record existed could be shared first by the background copier, which created the record and released it at once; the live window read that as a released chat, refused every later send with `This chat is available to continue here.` and branched its turns off the record the other computer reads, so even a restart left the chat unseen there. Is true now: a drive that has not made its first publish (fence 0) is not displaced by a record that names no other computer; its first publish takes the record over and every turn lands on the chat's own record."
---
