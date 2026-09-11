---
kind: fixed
title: a tab and its switcher row say the same thing, and an unnamed chat has one name
pr: 720
surface: [chat]
invalidates:
  - "The tab strip and the ctrl+k switcher were believed to draw one state per conversation. They read it twice: the strip took the watcher's fold of every task notice, the card took the turn and job flags alone plus the project index, which counts only nodes that are running. A conversation with a queued node drew `◐` on its tab and `○ … nothing new` on its row on the same frame. There is one reading now — `behindWatch.signal` for a held conversation, `frontSignal` for the one in front — and both surfaces ask it."
  - "The switcher's row for the conversation you are standing in never carried a mark, whatever was happening in it: `hopFront` set neither `needs` nor `moving`. It wears its own tab's mark now and keeps `you are here` as its note."
  - "The switcher's note and its mark were decided separately, so a row could say `nothing new` beside the working mark. `hopNote` takes the same signal the mark comes from: `3 tasks running` where the index has a count, `working` for work it cannot count — a queued node, a background job — and never the quiet words over either."
  - "An unnamed chat was called `Untitled` on its tab and in its breadcrumb root and `new conversation` on the switcher and the entry line. It is `new conversation` everywhere now; `Untitled` is not a word this surface has. `main` is still the conversation as a place (`esc/← main`, `say it to main`) and the `+` page's tab still says `New chat`."
  - "`internal/manual/chat/screen.md` said \"`Untitled` labels an unnamed tab and its breadcrumb root\" and that the switcher says something else. It names the one word, and says the card's rows carry the tab's two marks from the tab's reading."
---

The defect was visible in one screenshot: `◐ Sweeping the Frame Budget` on the strip
and `1 ○ Sweeping the Frame Budget · nothing new` on the card a keystroke away, with
the chat in front called `Untitled` above and `new conversation` below. Two answers to
one question, and two names for one thing, is what a person reads as the program not
knowing.
