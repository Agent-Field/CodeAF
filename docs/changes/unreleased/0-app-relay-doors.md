---
kind: added
title: Desktop engines pair computers and continue chats from other computers
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "The desktop engine previously required terminal pairing; it now serves all four pairing modes with explicit questions and cancellation."
  - "The app engine previously exposed only held-chat location; it now lists chats on other computers and conditionally continues them here."
  - "Finished cell chats previously waited for another turn before desktop publication; paired app starts and pairing completion now offer them through the shared background publisher."
  - "Desktop inventory previously changed relative save-age sentences on unrelated version bumps; fixed save timestamps now leave unchanged rows quiet."
---
The additive app wire reports the resolved Relay address and source, retains the
last pairing operation, and sends device and sync changes after pairing. Joining
with existing chats asks before replacement and requests an engine restart when
the computer's shared keys change. Live work blocks joining.

One shared inventory follower supplies versioned chat changes and removals;
presence-only changes reuse cached rows. Local agent list and open responses
use cached locations without waiting for relay reads. Continue here uses the engine's observed
computer and fence and returns a local app agent. A stale choice reports the
engine's lost-race sentence. Older home-folder chats remain local. Kept branches
and remote crew membership are outside this inventory.

Desktop pairing uses app sentences and labelled choices while keeping option
ids stable. An idle open chat says open on its computer, without claiming a
turn is running. Move failures use a person sentence with the technical cause
in error detail. Ownership recovery after a failure following acquisition
remains unresolved; releasing the acquired lease does not restore its predecessor.
