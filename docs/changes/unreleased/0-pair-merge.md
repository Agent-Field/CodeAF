---
kind: changed
title: Pairing keeps and shares a solo computer's existing chats
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "A computer with its own chats previously had to replace its shared state when pairing. A solo computer now keeps its chats, history, saved keys, memory and preferences and shares them with the other computer."
  - "The app previously asked whether to replace this computer's chats. Ordinary pairing now asks only for the existing pairing approval and keeps local chats."
---

Code and link pairing merge a used solo computer into the shared set. Existing
histories are exported under the shared keys with their original chat ids;
each computer keeps its own chats here, and Continue here moves a chat as before.
The approved local switch resumes after interruption. Background publication
retries failed uploads and repeats safely on the next start, using existing
chat events and sync notices.

Saved-key writes and the local switch share a per-home lock, and interrupted
explicit replacement records its phases so recovery keeps newly created state.
Background publication cannot release a chat opened meanwhile. Completed chats
are not retried; pending uploads use increasing pauses outside inventory locks.
The app's pairing state pass stops after success and stays off for homes without
an unfinished merge, preserving zero periodic requests from this worker when idle.
Terminal joins say sharing finishes the next time codeaf runs. Older chats outside
the recent background window appear on the other computer once opened here.

A used computer already sharing another set refuses without changing its files.
Explicit terminal `--replace` remains a confirmed clean-slate choice. The app
still requests an engine restart after joining and uses the existing protocol.
Explicit link re-pairing in the same set installs a new device so a removed
computer can return; ordinary same-set pairing still changes nothing.
Pairing completion wakes one new background chat scan, so chats saved after
startup are shared even when the approving computer keeps its identity and device.
