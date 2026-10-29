---
kind: changed
title: Desktop chats fold streamed text before it is framed
pr: 2043
surface: [engine]
invalidates:
  - "Previously every streamed chunk of a desktop chat crossed the remote framing as its own event frame; now a chat's engine folds text for up to 33 ms before framing it, and other surfaces keep one frame per event."
---

A desktop conversation's engine sends the first chunk of streamed text at once
and folds the chunks that follow for up to 33 ms, or until about 4 KiB, before
it frames them. Tool calls, questions, stops, errors and the turn's end send the
held text first and are never delayed. The text, its order and replay are
unchanged; at 40 tokens a second a stream reaches the window about 30 times a
second instead of 40, at a lower engine cost per stream. Terminal and remote
surfaces are unchanged.
