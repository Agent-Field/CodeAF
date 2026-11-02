---
kind: added
title: Each agent has a stable DID that its commands see as CODEAF_AGENT_DID
pr: 2027
surface: [engine, chat, docs]
invalidates:
  - "The engine gave commands no way to tell which agent ran them. Each conversation now carries a did:key identity in CODEAF_AGENT_DID, and task workers carry it with a #w<task id> suffix."
---

The DID is minted when a conversation opens and written with its next
identity write (opening itself still writes nothing), kept in its sealed
session truth, and listed as the optional `did` field on app agents. It uses
AgentField's did:key spelling. No private key is kept.
