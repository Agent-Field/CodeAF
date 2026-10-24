---
kind: changed
title: memory is one strength, every kept line rides every conversation and nothing changes it silently
pr: 1830
surface: [chat, engine, remote, docs]
invalidates:
  - "A memory had two strengths: routed lines a router picked per message, and rules marked always (at most 12). Now every active line in scope rides every conversation's message[0], bounded at 4,800 runes (store.MemoryBlockRunes). There is no always flag, no ranking and no use counts."
  - "Nothing measured a memory change against the bound across conversations. The store now refuses, inside the write transaction, any change that would push ANY conversation's block past it — a new line, a replacement, a rewording, a move or a put-back — and a line for the person or this machine is measured against every project's block. The receipt says whose block from the conversation saying it: `memory is full here`, `for <project>`, or `in another project`. A put-back or rewording onto words its owner already keeps is refused with `already kept` (store.ErrMemoryKept)."
  - "A store an earlier build filled past the bound now lets that build's rules, then the project's lines, then the machine's, then the person's ride, newest first; the conversation says once how many lines don't fit, and the Memories tab counts them."
  - "The model's remember asked first when it kept a rule (internal/approval/rule.go's consent floor). That floor is removed; the model's remember and forget act only on a turn the person typed (a woken turn is not one until the person types into it), and each says what it did."
  - "/always kept a rule without asking and listed only rules. It is now the same command as /remember, and bare /always lists every line kept here."
  - "/forget <words> forgot the single best full-text match without asking. There is no /forget command any more: a line is forgotten on the Memories tab (f or delete) and put back there (u), or by asking the model, whose forget lists several matches and forgets nothing rather than guess. Remote wire Memory.ForgetQuery is gone with it."
  - "The memory.enabled row (\"remember across chats\") turned memory off and needed a restart. It is now \"notice things to remember\": it governs only automatic noticing, it is read live from the next turn, and kept lines still ride with it off. A person who had it off before the upgrade is told once per machine, on the first start, that their N kept lines ride again."
  - "Automatic extraction kept lines silently. Noticing keeps at most one line per turn the person typed, and always says `noticed and remembered · <line> · <where> · see or forget it on the Memories tab` — or, for a correction already kept, that the old line is let go. A line noticed while its conversation is out of sight waits and is drawn there when the person comes back (session.Event.Unrecorded)."
  - "The tab was called Memory, the manual said it was not on the tab bar, its shelf headings looked like its lines, enter opened and closed shelves, its rows said `helped N · bore on N`, and alt+s walked its shelves. It is the Memories tab (typing `memory` still finds it), always on the tab bar, drawn like the Activity tab: groups fold on → and ← (never enter), lines hang on tree connectors with `kept by` and `age` columns, a pane reads the cursor row out on frames of 110 columns or more, enter opens a line's card, and alt+s is gone. Eight tab words now need 86 columns, not 84."
  - "Memory ran a pre-turn recall, a background tidy (role consolidate), contextual evidence, observed-outcome advisories and the memory_evidence tool. All of that is removed; the consolidate role is no longer registered, docs/contextual-memory.md is retired, and the memories_fts table is left in place this release for older engines still running."
  - "Remote wire Version was 21 with Memory.SetAlways and Memory.List. It is 22: those two are gone, Memory.Update carries only the words, and Memory.Move and Memory.Project are new."
  - "A far engine with no memory said `memory is off on this machine`. It now says `memory is unavailable on this machine · the store could not be opened`."
  - "The fixed-prefix waivers were 4,691 full and 13,413 lean after #1829. With memory_evidence off the belt and forget beside remember on the full one, the prefix measures 52,497 and 44,893 bytes, and the waivers fall to 4,497 and 13,393 (PERF.md, internal/session/prefixbudget_test.go)."
  - "The model's forget asked for approval. It is seeded allow beside remember, because both say what they did in the conversation and the Memories tab takes it back."
---

Memory should be simple and trustworthy: what the model
knows about a person is what the Memories tab shows, every change says so
in the conversation, and every change can be taken back.
