---
kind: changed
title: memory is one strength, every kept line rides every conversation and nothing changes it silently
pr: 1830
surface: [chat, engine, remote, docs]
invalidates:
  - "A memory had two strengths: routed lines a router picked per message, and rules marked always (at most 12). Now every active line in scope rides every conversation's message[0], bounded at 4,800 runes, and a save that would overflow is refused rather than dropped later. There is no always flag, no ranking and no use counts."
  - "/always kept a rule without asking and listed only rules. It is now the same command as /remember, and bare /always lists every line kept here."
  - "/forget <words> forgot the single best full-text match without asking. There is no /forget command any more: a line is forgotten on the Memories tab (f or delete) and put back there (u), or by asking the model, whose forget lists several matches and forgets nothing rather than guess. Remote wire Memory.ForgetQuery is gone with it."
  - "The memory.enabled row (\"remember across chats\") turned memory off and needed a restart. It is now \"notice things to remember\": it governs only automatic noticing, it is read live from the next turn, and kept lines still ride with it off."
  - "Automatic extraction kept lines silently. Noticing keeps at most one line per turn the person typed, and always says `noticed and remembered · <line> · <where> · see or forget it on the Memories tab`."
  - "The tab was called Memory, the manual said it was not on the tab bar, its shelf headings looked like its lines, enter opened and closed shelves, its rows said `helped N · bore on N`, and alt+s walked its shelves. It is the Memories tab (typing `memory` still finds it), always on the tab bar, drawn like the Activity tab: groups fold on → and ← (never enter), lines hang on tree connectors with `kept by` and `age` columns, a pane reads the cursor row out on frames of 110 columns or more, enter opens a line's card, and alt+s is gone. Eight tab words now need 86 columns, not 84."
  - "Memory ran a pre-turn recall, a background tidy (role consolidate), contextual evidence, observed-outcome advisories and the memory_evidence tool. All of that is removed; the consolidate role is no longer registered."
  - "Remote wire Version was 21 with Memory.SetAlways and Memory.List. It is 22: those two are gone, Memory.Update carries only the words, and Memory.Move and Memory.Project are new."
  - "A far engine with no memory said `memory is off on this machine`. It now says `memory is unavailable on this machine · the store could not be opened`."
  - "The model's forget asked for approval. It is seeded allow beside remember, because both say what they did in the conversation and the Memories tab takes it back."
---

The owner asked for the simplest memory that can be trusted: what the model
knows about a person is exactly what the Memory tab shows, every change says so
in the conversation, and every change can be taken back.
