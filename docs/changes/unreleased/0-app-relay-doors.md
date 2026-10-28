---
kind: added
title: Desktop engines pair computers and continue chats from other computers
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Recovered job checkpoints previously left stale process files running and reopened background chats; a running record whose ending the checkpoint already holds no longer counts as unfinished, and the next held recovery writes that ending."
  - "Conversation model identities previously stayed in a local index, letting a take back reuse older model slots; the saved main and fallback now travel with the chat."
  - "Preparing an unavailable incoming main model previously recorded a generic change and its specific reason; the change now records and publishes only the specific reason."
  - "A missing local default previously refused a take after acquiring and installing the chat; taking now completes and the first send explains model setup without a provider call."
  - "A restarted relay previously reported unobserved paired computers offline immediately; presence now remains unknown for one default presence window unless an observed connection actually lapses."
  - "An open desktop view previously lost its writer to the idle sweep and read calls acquired a new fence; explicitly open views now renew the same hold until closed."
  - "An expired own hold previously rejected newer locally saved work after a relay outage; unchanged ownership now renews and publishes that work."
  - "A failed renewal of an expired own hold previously blocked local work; unreachable renewals now leave outage turns writable and saved for retry."
  - "A taken chat previously sent an unavailable provider model to the receiver's router; it now checks provider readiness and explains its usable local default."
  - "The desktop engine previously required terminal pairing; it now serves all four pairing modes with explicit questions and cancellation."
  - "The app engine previously exposed only held-chat location; it now lists chats on other computers and conditionally continues them here."
  - "Finished cell chats previously waited for another turn before desktop publication; paired app starts and pairing completion now offer them through the shared background publisher."
  - "Desktop inventory previously inferred offline from an expired chat hold; offline wording now requires an explicit offline presence fact."
  - "Desktop saved times previously came from relay publication; new publications carry the sealed head's turn time, and takeover text leaves dates and ages to the desktop."
  - "Desktop inventory reads previously emitted chat changes; list calls now stay silent and the follower compares actual last-emitted content."
  - "Synced model choices previously applied without a usable local provider; skipped choices now stay skipped until a newer choice arrives."
  - "Opening an existing local hold previously acquired another fence; startup and idle ownership checks now renew an unchanged hold."
  - "Presence refreshes previously ignored the reconnect grace period and could emit older last-seen times; queries now agree with watch events."
  - "Excluding takeover staging folders from desktop admission did not release the store daemon's directory watcher; fetched copies now finish materialization in a one-shot process before installation."
  - "Following an installed chat previously spawned the engine inside its workspace; empty or long Windows copy paths now use an existing local store directory and pass the workspace explicitly."
  - "Taking a project chat back on Windows could fail restoring receipts beyond MAX_PATH; the engine now receives an extended-length composed-directory path."
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

Incoming model slots use the engine's resolved provider credentials and local
connections. An unavailable provider leaves the local choice and its edit clock
alone; the skipped revision is remembered locally, preventing sync flaps and
retroactive adoption after provider setup. Effort and limits still travel.

An idle holder renews its existing fence through watch reconnects and resumes.
Continue here accepts a newer fence from the same holder only when the saved head
is unchanged; different holders and newer heads remain conflicts. Older relays
retain their exact-fence wire. Startup device events wait for the complete roster,
all device emitters compare their last value, and last-seen times never regress.
Presence queries honor the watch's reconnect grace period. Reconnecting without
changing a chat row emits no chat change. A completed turn after takeover is
published promptly while its holder stays open.
Incoming copies also finish materialization in a one-shot store process, releasing
its directory watcher before Windows renames the staging folder into place. The
engine follows the installed folder for later edits and saves. Failed installations
retain the existing lease release behavior.
Follow and later dirty checks pass the installed tree as an engine argument,
running from the local store so long Windows copy paths can be rebound. Missing
empty workspaces are created before binding; incoming chats use their receiver
workspace instead of an unavailable absolute path recorded on the sender.
Taking a project chat back on Windows also restores composed turn receipts whose
full paths exceed MAX_PATH, using the native extended-length path spelling.
Guarded materialization preserves caller-supplied non-daemon transports, including
their binaries and runners. Shared-daemon routes still finish in a one-shot
process before installation so no staging-tree watcher survives the move.

Relay recovery renews an unchanged own hold even when locally saved work is newer
than the relay's head. Failed uploads retain the existing bounded, single-flight
retry loop; neither a failed renewal nor an expired own lease says this computer
continued the chat elsewhere. Another computer's successful continuation still
fences this window and preserves its pending work.
Incoming chats reuse the shared provider-readiness check before choosing a local
default, explain the change once, and apply the existing conversation model-switch
rules. A computer without a usable default gets a plain setup reason.
The saved model choice also identifies its provider before any call, so a newly
selected custom alias cannot fall through to a different computer's router.
Taking a chat without a usable default now returns its local agent and reconciles
the chat row's local id immediately. The first send gives a plain model setup
reason once. After a usable default is set, the next send uses it and explains
the change once. Actionable take reasons stay in the visible error message;
technical detail has no repeated engine prefixes.
Native and hosted relays omit unobserved computers for their first 75 seconds
of presence memory, while still reporting observed connection lapses. Restarting
the relay cannot briefly label a continuously running peer offline.

An explicitly open desktop view keeps its hold through idle sweeps; heartbeats
renew the same fence without row churn. Conversations opened only for background
work still release after ten idle minutes and re-acquire when work needs them.
Hold receipts also
require a nonempty home, so a misconfigured sync cannot write relative lock files
into the source tree. The stale-drive refusal fixture now uses a temporary home.

Desktop relay cancellation goroutines now use the shared panic guard. Inventory,
pairing, continuation, device publication and drive retry critical sections defer
their unlocks while retaining their existing lock order and work boundaries.

A running process record whose ending the task checkpoint already holds no longer
counts as unfinished; that check only reads, so a chat held elsewhere is never
written to. Unfinished or ambiguous work still enters recovery, which durably settles
both the task checkpoint and process history. Idle release no longer triggers a
spurious conversation open, watch reconnect, presence update or settled-vault send.

Taking a chat back replaces its remembered local model with the installed saved
choice. Main and fallback travel together; a usable main stays selected silently,
an unavailable main uses the receiver's usable default with one explanation, and
a missing usable default retains the existing setup sentence. Index rebuilding
preserves the saved choice instead of reinstating the journal's original model.
