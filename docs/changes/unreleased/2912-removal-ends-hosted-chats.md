---
kind: fixed
title: Removing a person ends their hosted chats' access and a chat's shared-area leftovers are cleared
pr: 2912
surface: [factory, engine, build]
invalidates:
  - "Removing a person from the organization ended their sign-ins and bots and was believed to end what their hosted chats could still do, but a hosted chat holds a model and git bearer of its own that lasts seven days, so a removed person's chat kept spending the organization's model key and reaching git as them, and their stored keys and chats stayed behind. Now removal ends those bearers at once, forgets their stored provider keys and deletes their chats on the chats' host. A chat the host could not delete is closed to everyone, guests included, and removing the person again deletes it."
  - "A hosted chat that was stopped, deleted or started afresh was believed to leave nothing behind but its own folder, but whatever its Linux user had left in /tmp, /dev/shm, /var/tmp and in System V shared memory, queues and semaphores stayed, and the next chat given that same user found it as its own. Now every mode of the reap helper removes those, as that user and without following links."
  - "A reconnect to a hosted chat made just after a connection to it ended could be refused as the chat being unavailable and read as The place chats run on is not answering. It no longer is, because a connection leaves the chat's group before it is cancelled."
---

Found by the ship-ready test round on the organization line: a hostile websocket client, sandbox-escape probes through a
chat's own tool path, the reuse of a Linux user after a chat is deleted, and concurrent spend caps. The keyless smoke's
list-row check also now expects the `verbs` the server sends. Nothing about a chat's limits, sharing or the model door
changed for anyone who was not removed.
