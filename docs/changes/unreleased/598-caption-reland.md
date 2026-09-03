---
kind: fixed
title: the caption wave returns, with its narrator off the scripted queue and its prompt paid for
pr: 598
surface: [chat, engine]
invalidates:
  - "#563 landed on dev and was reverted the same day (#591), so a memory of captions on the conversation is right about the feature and wrong about the trunk. Its behaviour returns here unchanged; what changed is the two things that made its gate red."
  - "The scripted completer in internal/session answered every provider call by its place in a step queue. The caption narrator arms half a second into any tool batch, so it took the step the turn was scripted to ride and three promote and steer fixtures read an empty tool result. The narrator is now answered off the queue by default, recognised by its own system line; a fixture that wants it ANSWERED installs an aside, which is still consulted first. The two namers keep their own opt-in helper, because three tests are about the namer itself."
  - "The system prompt's `# Things that keep working after this window` section opened with the recognition warning, the discharge test and the anchoring rule. All three are said at greater length by `stand`'s own description (tools_standing.go), which rides in front of every request carrying the tool, and the section says in its first line that it is about that tool. It now begins at WAKING OR HOLDING: the prompt goes 21,495 to 20,712 bytes and the fixed prefix is 47,531 against an unchanged 48,000 budget."
---
