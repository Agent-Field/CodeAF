---
kind: changed
title: Relay coordination uses three voters and joined computers can lead
pr: 0
surface: [chat, remote, build, docs]
invalidates:
  - "One relay directory writer previously decided each identity's changes. The hosted relay and codeaf relay --store now use a three-voter Raft group per identity, with majority commitment and fresh authority reads."
  - "Paired computers previously used only the directory API. Open joined computers now keep durable learner copies without increasing the voting quorum; worker churn does not add voters."
  - "Joined computers had no leadership commands. codeaf cluster status now reports committed membership and the current leader, and codeaf cluster lead safely replaces one anchor vote, transfers leadership and returns an elected read receipt; saved requests retry unchanged."
  - "Coordination depended on the relay's single writer. Two remaining anchors can now elect a fallback and restore the third after a personal leader disappears, targeting about 65 seconds in healthy conditions; a returning computer remains a learner and never automatically regains leadership."
  - "A majority was not required by the former directory writer. Losing two of the three current voters now refuses coordination and preserves saved chats instead of creating a writable minority."
  - "Caught-up open homes previously polled shared state. Idle homes now send zero HTTP requests, peer or forwarding frames, or client attempts after all voters apply the idle barrier; presence sweeps recur only with a viewer and fresh input requires a new majority response."
  - "Older clients could blindly force a live chat without its observed holder. Both relays now refuse that request with another device continued this chat first; releasing the chat or updating the client permits safe continuation."
  - "Legal older directory requests could be refused above the smaller coordination envelope. Both relays now retain the original 1 MiB HTTP limit, stage large signed bodies and commit complete metadata atomically, recovering interruptions without exposing partial changes or deleting retained records."
  - "Workers previously needed personal-device trust for shared access. Delegated workers now have expiring selected-chat grants and conditional assignment claims; they cannot approve pairing, read the identity vault, rotate it, enroll devices or lead, and terminal worker setup is not available yet."
  - "Reopening retained work could compare its branch fence with the original chat and falsely refuse tools. Ownership checks stay with the record each window opened, and replacing a displaced drive settles its previous writer before starting another. A failed final save remains failed for every close caller."
  - "Deferred local durability could resolve a fallback daemon against a different current home. Both engine routes now retain the home and engine selected at capture."
  - "A single directory database was formerly the live source. First use now freezes and imports it into the three anchors while retaining the source, and the one-way upgrade cannot return to that writer after cluster changes are acknowledged."
  - "Self-hosted identities were described as limited only by disk space. Coordination records now have an 8 MiB default allowance, with larger imports retaining their measured allowance; capacity refuses growth without deleting saved data."
---

Leadership moves coordination metadata while chats keep their ordinary ownership,
encryption and save rules. Three voters tolerate one unavailable member; local
anchors share one host, and hosted anchors share their provider and relay path.
Phone leadership and terminal worker provisioning are not available yet.
