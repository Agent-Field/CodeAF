---
kind: fixed
title: A removal the chats' host could not finish is retried; no plain-HTTP warning behind a proxy
pr: 2916
surface: [factory, docs]
invalidates:
  - "Removing a person while the chats' host was down was believed to be finished by removing them a second time, and the operator README, docs/organizations.md and the manual said removal never retries on its own, so their chats' Linux users and folders stayed on the machine until then. Now the removal writes a durable mark first, and the server retries the deletes by itself, starting five seconds later and waiting twice as long each time up to five minutes, across a restart, until the host confirms every one. The operator does not repeat the removal, and a chat made later by someone who joined again under the same handle is not deleted."
  - "Removing a person was believed to leave the closed chats for the operator to remove again, so the server logged that removing the person again once the host answers deletes them. The server now deletes them itself, and its log says so when it starts waiting, again after the tenth failed try and every power of ten after, and once when it finishes."
  - "Every start of `codeaf factory serve` on a non-loopback address printed the plain-HTTP warning, and the operator README called it expected behind an HTTPS proxy. It is now quiet when trusted_proxies lists the proxy, and prints exactly as before when none is listed."
---

Found by the test round on the hosted organization line: the host-down removal was its open call 1, and the Compose walk found the warning reading as a failure behind a proxy.
