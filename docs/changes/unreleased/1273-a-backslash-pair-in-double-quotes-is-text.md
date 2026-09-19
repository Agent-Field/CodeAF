---
kind: fixed
title: a backslash pair inside double quotes is text to the one-command reader
pr: 1273
surface: [engine, chat]
invalidates:
  - "A declared check holding a backslash inside double quotes was refused as more than one command, whatever the backslash stood before. A search pattern written `\"a\\|b\"` is one command to the shell and was turned away, naming the backslash."
  - "The reader that finds where a line's first stage ends had no escape rule at all, so it read an escaped quote as the end of the quotation. The two readers now share one scanner and cannot disagree."
  - "Inside double quotes a backslash and the character after it are text. A dollar or a backtick there still refuses the check. A backslash outside quotes, one that ends the line, one before a newline, and a quotation left open all still refuse it. A doubled backslash before the closing quote does not hide that quote, so what follows it is still read as composition."
  - "The longest command a declared check may be moved from 200 bytes to 256. The measured check was 203 bytes, and the refusal for length names no cause."
---

A person sees no new word. The refusal sentence and the checks schema description
are unchanged. The manual's paragraph on declared checks now says what a backslash
inside double quotes does.
