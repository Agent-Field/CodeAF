---
kind: fixed
title: a reconnect reuses its registered identity instead of introducing itself again
pr: 1236
surface: [chat]
---
On reconnect, when a service's saved registration had used a fallback loopback
redirect, the client could bind a fresh redirect the service did not know, so its
identity check failed and it introduced itself a second time. The reconnect now
tries the saved registration's redirect first, so when that address is free it
reuses the identity the service already has and introduces once. The fixed
callback ports and their order are unchanged.
