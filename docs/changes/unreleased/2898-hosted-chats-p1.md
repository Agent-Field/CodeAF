---
kind: added
title: Hosted chats get a metered model door, sealed personal keys, server-side git and a port limit
pr: 2898
surface: [chat, engine, factory, build, docs]
invalidates:
  - "The organization server's /v1/model/ door passed a chat's call straight to the upstream and counted nothing, a chat's model access was kept in the server's memory for twelve hours and a restart of the server ended it, and a hosted chat's commands could connect to any port of the network its machine was on. Now every call is metered to the cent into the person's My space (so My space's spent_usd and at_limit count hosted chats), a chat stops at CODEAF_HOSTED_SESSION_CAP_USD (default 5) and a person at the monthly limit, the access is kept as hashes for seven days and survives a restart, and a hosted chat's commands may connect only to the ports in CODEAF_SESSIONHOST_CONNECT_PORTS (default 443,80,22,9418) and the organization server's own. With hosted chats off nothing changed."
  - "A person could not give a hosted chat a GitHub access or a model key of their own, and the chat had no way to reach git. A person can now store them (PUT /v1/hosted/keys/{provider}, sealed on the server under CODEAF_SERVER_KEY, never readable by a chat), and a chat's git clone and push of an allowed host go through the organization server, which adds that access to each request."
  - "A machine whose kernel is older than Linux 6.12 refused to start chats without saying how to accept that. The refusal now names CODEAF_SESSIONHOST_REQUIRE=uid, and deploy/org's README says Linux 6.12 or newer."
  - "A provider that rejected the organization's model key (a 401 or 403) made a chat's model call come back as a 502, which the engine retried quietly about once a minute while the server's log said nothing. Now the chat reads one plain sentence in its transcript and is not retried, and the server's log gets one line naming the cause, never the key; a person's own stored key that is rejected is worded as theirs."
---

A hosted chat can now be given to people without handing them the organization's key. Its model calls go through the
organization server, which sends them upstream with the real key, counts each one to the cent into the person's
My space, and refuses in a plain sentence at the chat's spending limit or the person's monthly limit. People can store
their own model key and GitHub access, sealed on the server; a chat's git goes through the server so the chat never
holds a GitHub token. The chats' machine limits the ports a command may connect to, so the other services on a
private network with no policies of its own are out of a chat's reach. The limit is by port and not by address:
`deploy/org/README.md` says what that does not cover.
