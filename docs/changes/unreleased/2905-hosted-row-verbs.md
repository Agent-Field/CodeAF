---
kind: changed
title: A hosted chat's controls follow the verbs the organization's server sends
pr: 2905
surface: [app, factory, docs]
invalidates:
  - "The desktop engine worked out a person's verbs on a hosted chat itself, from the default policy's role table: the owner of a chat held every verb, a guest held the verbs of the default bundle named by the row's role, and an unknown role held view. That was only right for an organization still on the default policy. Now the organization's server decides: every row of `GET /v1/hosted/sessions`, own and shared, and the answer that makes a chat carries `verbs`, the verbs the caller holds on that chat under the policy as it stands when the list is read, and the engine shows exactly those. The engine has no role table any more."
  - "A hosted chat row from a server that sent no verbs was shown to its owner with every verb. Now a row with no verbs is shown as view only, for another person's chat and for the person's own. That only hides controls: the server's wire refuses what the person does not hold, and nothing on the client enforces anything."
  - "The hosted list's rows said who owns a chat, the caller's role and the kind of access, and a client had to turn the role's name into verbs. Now each row says the verbs themselves, in the policy's order. An operator-only row holds none, and a bot's rows hold its person's verbs narrowed by its scope."
---

The wire's enforcement is unchanged: the server still decides on every call and re-reads the policy on every
heartbeat, and the desk reads the same `hosted.verbs` list as before. One known limit: an owner whose policy holds
`run` is attached by the byte-for-byte proxy, which has no verb table, so their row names what the policy gives them,
which can be fewer verbs than the wire would let through. That only ever hides a control.
