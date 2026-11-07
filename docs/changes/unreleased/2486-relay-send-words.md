---
kind: changed
title: A message to another computer says why it did not go: relay unreachable, or outbox full
pr: 2486
surface: [app, engine, docs]
invalidates:
  - "chats.send with an unreachable relay answered the transport's own error as an engine error; it now answers code unreachable with a sentence saying the message didn't go and to send it again."
  - "The outbox refusal said eight messages were waiting for 'the other computer'; the limit is this computer's across every chat and computer, and the refusal now says so."
---

The desk (codeaf-desk#280) keeps the draft and says the same thing in the composer.
A live two-computer e2e on deepseek-v4.1-flash pins that a message waiting for a
computer that goes offline runs on this computer after Continue here.
