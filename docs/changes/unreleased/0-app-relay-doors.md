---
kind: added
title: Desktop engines pair computers and continue chats from other computers
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "The desktop engine previously required terminal pairing; it now serves all four pairing modes with explicit questions and cancellation."
  - "The app engine previously exposed only held-chat location; it now lists chats on other computers and conditionally continues them here."
  - "Finished cell chats previously waited for another turn before desktop publication; paired app starts and pairing completion now offer them through the shared background publisher."
  - "Desktop inventory previously inferred offline from an expired chat hold; offline wording now requires an explicit offline presence fact."
  - "Desktop saved times previously came from relay publication; new publications carry the sealed head's turn time, and takeover text leaves dates and ages to the desktop."
  - "Desktop inventory reads previously emitted chat changes; list calls now stay silent and the follower compares actual last-emitted content."
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
in error detail. After a failed installation, the released lease names its last
holder but does not make a missing local copy appear here. An unchanged source
chat can acquire a newer fence on its next send; fences are never rolled back.

Synced preferences now notify desktop Settings once with its full current value.
Identical computer lists no longer repeat Devices events. Models whose catalog
disallows reasoning receive no reasoning parameter. An unlisted model's specific
thinking refusal gets one immediate repair without reasoning, remembered for the
process; a permanent capability refusal does not retain a retryable app turn.
Private takeover staging folders are excluded from app listing and automatic
recovery, preventing copied working presence from opening them before installation.

Startup publication preserves its signed quiet release through the native relay's
presence wrapper, leaving no live lease or unauthorized release. Unknown computer
presence and unknown saved-turn times are omitted. A closed chat makes no claim
that its computer is offline; unchanged chat rows stay quiet across inventory reads.
Turn-time metadata is sent only when the relay advertises support, preserving
publication on older relays. Historical records are not backfilled at startup.
