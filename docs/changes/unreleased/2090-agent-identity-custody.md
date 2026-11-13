---
kind: added
title: Agent identities retain encrypted signing and encryption keys
pr: 2090
surface: [chat, engine]
invalidates:
  - "An agent DID used to be a public name only; new conversations now retain encrypted signing and encryption keys."
---

Each new conversation keeps a v1 identity document in its sealed truth, including
Ed25519 and companion X25519 public keys and encrypted private-key custody.
Purpose-separated signing verifies with the public DID alone. Commands and
`agents.list` still expose only the public DID. A copy or branch mints fresh keys.

Public-only identities migrate on the next metadata write without changing their
DID. Their missing signing key remains explicitly unavailable; they gain encrypted
X25519 custody. Opening an old folder writes nothing. Unknown document versions
remain byte-identical across metadata writes. Agent key rotation is not included.
