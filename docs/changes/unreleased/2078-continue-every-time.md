---
kind: fixed
title: Continuing a chat from another computer works every time, and codeaf doctor can repair sync
pr: 2078
surface: [chat, engine]
invalidates:
  - "After one continue in a window, a second or third continue here could do nothing, because the running-take mark was not cleared on every ending; it is now cleared when a take opens, is brought forward, fails or is cancelled, and a press during a take says a chat is already continuing here."
  - "A failed continue closed its card and said why only on Home's say line; the failure now stands on the card with 1 retry and 2 leave it there, or in the chat."
  - "A busy engine store failed a continue with the generic could-not-be-put-here sentence; it now names the program holding the store and its pid, and offers retry."
  - "Under codeaf chat --host, Home offered continue here and showed chats the other computer held as held elsewhere, acting on this computer's disk; under --host continue here is not offered and those chats are not held elsewhere."
  - "One chat could show twice on Home, once from this computer and once through sync, and each branch copy had its own row; rows are keyed by chat id and copies fold into the chat's row with a count."
  - "codeaf doctor said nothing about sync; it now reports a busy store, unfinished continues, duplicate rows and lapsed holds, and codeaf doctor --fix repairs what is safe."
---

`codeaf doctor --fix` releases unfinished continues this computer started and
retires branch copies whose turns are all already in the main chat, printing a
`fixed:` line for each. It never touches another computer's live hold and never
removes a turn that exists only in what it would delete. A store lock whose
process has ended is already free, so doctor neither reports nor removes one.
