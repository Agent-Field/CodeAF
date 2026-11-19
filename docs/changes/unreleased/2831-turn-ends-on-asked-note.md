---
kind: fixed
title: A reply that is only the note codeaf asked for is sent back to do the work it names
pr: 2831
surface: [chat, engine]
invalidates:
  - "The silent ladder's note told the model `Before your next tool call, write a short visible note`, and the write-your-notes rule's held demand told it to write the note `as an ordinary visible message`. A model that sent the note alone, with no tool call, ended its turn on it. Both now ask for a note starting with `[update]` in the same reply as the next tool call, and say a reply with no tool call ends the turn."
  - "Only a reply opening with `[update]` was sent back for stopping mid-work (#2818). A tool-free reply that directly answers codeaf's `[silent]` note or `[held]` demand is now sent back too, marker or not, with the notice `it wrote down where it was and stopped · asking it to go on`. The continuation offers `[no change]`, so a reply that was already the final answer keeps its place after one short call. It shares the update nudge's budget: at most two send-backs a turn for both together. A question to the person, or a reply waiting on work already out, is left alone."
---

Measured on the connected-canvas qloop with deepseek-v4.1-flash. In nine of
nine stalled turns from the baseline through run 5 ("Where I am: … Next: make
two charts", "Learned: … Next I'm checking whether the logo PNG has a
transparent background"), the final reply came straight after codeaf's own
request for a note, and nothing was built.
