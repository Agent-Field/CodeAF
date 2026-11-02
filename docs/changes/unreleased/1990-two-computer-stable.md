---
kind: fixed
title: Two computers stay in step - fast take-back, moved task pages, honest saving
pr: 1990
surface: [chat, engine, remote]
invalidates:
  - "Continuing a chat after its holder was killed took 6 to 16 seconds; the take now acquires before it restores the folder, and lands in about two."
  - "A finished task in a chat that moved to another computer had no plan page there; it now opens read-only from its run receipt, steps and worker transcript."
  - "The sealing notice said saving works again after any later seal; it now says so only once this chat's own turns are sealed and published."
  - "A failed final save was printed only to the engine's log; it is now one plain sentence on the terminal after the screen is restored."
  - "Keys typed while /new opened cancelled it and were sent in the chat being left; they now stay with the new chat, and Esc gives them back unsent."
  - "The computer holding a chat said nothing while the relay was unreachable; it now says once that the chat is not syncing, and again when syncing is back."
  - "codeaf cluster status printed a raw Go error when the relay was down; it now prints the same plain sentence as codeaf devices."
  - "The embedded storage engine was furrow 0.3.4, which could unlink a newer daemon's socket; it is now furrow 0.3.5, which removes only its own."
  - "codeaf devices printed a false nothing-is-paired line and exited 0 when the relay was down; it now prints only the unavailable sentence and exits non-zero."
  - "A pairing that timed out showed raw Go error text; it now says plainly that the sync service did not answer in time."
---
Fixes the defects the real two-computer relay tests found after the workspace
work landed. Closing a chat no longer freezes the window, Home never offers to
continue this computer's own chat, a moved copy keeps its label after this
computer is removed, and the first Enter in a copy held elsewhere no longer
claims other machines are unreachable while the relay answers.
