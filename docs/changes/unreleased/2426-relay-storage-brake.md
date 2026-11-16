---
kind: changed
title: A full hosted relay turns newcomers away instead of stopping sync for everyone
pr: 2426
surface: [chat, remote]
invalidates:
  - "The hosted relay's globalStoreBytes refused every identity's new frames once the deployment total passed it; it now closes admission to new identities only, and the stop-everyone switch is the new opt-in globalHardStoreBytes (default unlimited)."
  - "At the identity's ceiling the chat said 'free space there and reopen this chat'; nothing on a computer frees relay space, so the sentence is now 'your sync space is full (5 GiB), so new turns stay on this computer'."
  - "relay_storage_full decoded to blobstore.ErrFull or directory.ErrFull and showed the identity's own full sentence; it now decodes to wireauth.ErrRelayFull, still matching each wire's general full error, and says 'sync is full for everyone right now, so new turns stay on this computer and go up when it has room'."
  - "A chat whose sync startup was refused (full, too many new identities, rate limited) never tried again until it was reopened; it now retries by itself after the refusal's wait, at most an hour."
  - "The manual said a full relay stops retrying until the chat is reopened and that a removed computer retries hourly; a full refusal is retried hourly while the chat is open, and a removed computer stops contacting the relay."
---
Nothing on the hosted relay is reclaimed while an identity lives, so a shared
ceiling that refused every write would have turned about 205 full identities
into an outage for everyone under the production 1 TiB setting. The ceiling
now bounds storage by closing the door to newcomers, and the operator keeps an
explicit emergency stop.
