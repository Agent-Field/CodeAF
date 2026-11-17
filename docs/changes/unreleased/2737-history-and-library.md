---
kind: changed
title: Documents keep History in plain words, and Add to Library never waits on learning
pr: 2737
surface: [engine, app, chat, docs]
invalidates:
  - "The manual and a workstream conversation's page said Files keeps no version history and Afi must never offer an earlier copy. Documents and canvases keep History with Restore, in Personal and organization workstreams (owner decision 4, 2026-11-17). ws_file_read on an organization's document now lists earlier_copies (when, and who by name), absent when there are none, and the agent may mention an earlier copy only when it lists one, never by number or id."
  - "knowledge.capture (a file's Add to Library, a passage's Keep in Library) was refused with learn from chats and jobs off. An engine that says the hello word knowledge.keep_always keeps the add whatever learning says and writes it into the Library then, scoped to that add alone (owner decision 5)."
  - "The Library's build read every saved conversation, a workstream's Afi and chats included, so turning learning on filed a suggestion the person had dropped (E3 F4). The build now passes over a conversation marked as a workstream's, as the turn's own capture already did."
  - "Library guides carried nothing saying whether learning wrote them. knowledge.snapshot heads and knowledge.read detail now carry from_chats, which the desk shows as From your chats."
---
