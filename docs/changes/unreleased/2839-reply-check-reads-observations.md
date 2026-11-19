---
kind: changed
title: the reply check reads what the turn's own steps returned, not only its looks
pr: 2839
surface: [engine, chat]
invalidates:
  - "Was believed: the reply check (#2836) compares a reply with the turn's newest look only. Is true: the replycheck reader now gets every look plus the turn's own step outputs (shell output, export answers, files read back) that share a word or figure with the reply, oldest first, later winning, entries before a possible change marked (earlier). It stays capped (4 KB reply, 9 KB looks, 3.5 KB outputs), one call and one send-back a turn, with [no change] kept."
  - "Was believed: a turn that changed the work after its last look is always sent back with the bare look-again, without a reader. Is true: when an output after the newest look shares words with the reply, the reader is asked first and its lines replace the look-again; with nothing relevant after the look, the old stale arm is unchanged."
  - "Was believed: the dim line for a contradicted reply reads 'its reply did not match its own last look'. Is true: it reads 'its reply did not match what it saw this turn · asking it to correct the reply'."
---

Measured on the qloop run16 and run17 transcripts with deepseek-v4.1-flash:
$0.0032 per checked turn (27 turns) before, $0.0075 (47 turns) after; most of
the rise is the reader's reasoning tokens. Claims that nothing covered are
deliberately not listed: that variant named something on 30 of 47 turns at
23% more cost, and acting on it would mean blocking.
