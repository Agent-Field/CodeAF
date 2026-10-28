---
kind: added
title: Desktop engines pair computers and continue chats from other computers
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "The desktop engine previously required terminal pairing; it now serves all four pairing modes with explicit questions and cancellation."
  - "The app engine previously exposed only held-chat location; it now lists chats on other computers and conditionally continues them here."
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
