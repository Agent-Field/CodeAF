---
kind: changed
title: Paired idle chats release their hold after upload and available chats open here automatically
pr: 2287
surface: [chat, engine, app, remote, docs]
invalidates:
  - "An open idle chat previously kept renewing its computer's hold. In a paired set it now releases after all saved state has reached the sync service."
  - "Selecting a released or lapsed remote chat previously required Continue here. Paired opens now acquire it automatically on terminal Home and through agents.open without displacing a live holder."
  - "The first Home open of a local copy after restart could bypass free acquisition while the shared listing caught up. Ambiguous paired readings now get a short ownership check, with local opening preserved on failure."
  - "Every paired local Home open briefly required a successful sync-service read. Answered display rows now open without that read, and an ambiguous reading waits at most two seconds before falling back locally."
  - "A listed free local chat could still require a reachable sync service to open. Home now opens the saved local copy when the listing or free-open ownership read is unreachable; remote-only chats explain the failure."
  - "A desktop take previously blamed another device for every ownership conflict. A newer hold by the same computer now says that computer picked the chat up again."
---

Running turns, queued or steering work, tasks, jobs, deferred writes and unsent
turns retain the hold. Task startup and parked queued tasks retain it too.
Saved settings and workspace edits made after the last seal are captured and
uploaded before release. Failed uploads retain the hold. Idle preparation is
canceled by new work and does not extend its ownership deadline. The original window stays
open as a reader and reacquires before accepting new work. If another computer
won the hold, it refuses without recording the work and names the new holder.
Automatic open restores through the existing transfer path with conditional,
non-forcing acquisition. Older builds' live holds still require Continue here.

Pairing is learned from existing device roster reads and watch events. Sync off,
unpaired and single-computer homes add no idle-release network requests. The
app engine exposes available shared chats through its open door; desktop
clients that only preview them need to call that door to use automatic opening.

Ordinary messages in the original released window reacquire in place, preserving
local ignored install folders without a restore or setup card. Opening a free
chat refreshes an already-open stale viewer too. Repeated moves keep an existing
local root and use bounded, stable copy-folder names on first arrival.
When this window has no open copy and Home has no reading for a local chat or
still shows this computer's old live hold, a paired home checks ownership for
up to two seconds. A failed or timed-out check opens the local chat as usual.
Answered display rows need no extra read, including a listed free chat or a
chat already open in this window. Released and lapsed paired chats restore the
latest head through conditional automatic acquisition at a new fence, including
the first open after a restart. Unpaired, sync-off and single-device homes add
no ownership read for local opening.

An unreachable Home listing opens saved local chats directly, including this
computer's released chats. A free take's initial directory read has the same
two-second limit; an unreachable read falls back to the local copy, or keeps
the existing unreachable refusal for a remote-only row. Successful transfers
retain their normal move budget and local opening keeps existing send admission.

Terminal certification obtains a complete workspace scope while the warm engine
keeps sealing turns. Saved conversation choices retake the idle hold before
acknowledging a change, or retain the requested choice on a held-elsewhere
refusal. Ordinary held tasks keep their existing offline admission, and
home-state exchanges serialize behind competing flushes under their own context.

Delayed idle-release observations cannot close the work gate after this window
has acquired a newer hold. Manual renames and other durable edits outside a turn
retake the released hold before writing. Idle comparison visits shared ancestors
once and retains the hold beyond its bounded complete-proof budgets. Closing
always stops the writer even when its budget expires, and model RPCs preserve
the factory's existing lent-view restriction.

Durable edits read the published drive without waiting on installation or
catch-up's mutex. Taking back an idle-released hold has a separate 15-second
write budget; ordinary held admission keeps its 750 ms read. A lost acquire reply
stays retryable and keeps the draft. A newer own-device fence cannot identify the
window that acquired it, so an uncertain window does not adopt or renew it and
never offers itself a Continue-here card. If nobody adopted the hold, retrying
after its ordinary lease lapses takes a new hold through the existing conditional
acquire.

Existing ownership expiry and consent rules still hold: an expired hold or one
released outside its window admits nothing until continued, reopening a chat
takes a new fence, and takes require the exact observed holder and fence. Only
a window that released its own hold retakes on new work, by conditionally
acquiring a new fence with the saved head unchanged; a heartbeat at the
released fence never revives work permission. No directory protocol or
replicated lease record changes are included.

A desktop take that loses to the same computer reclaiming an idle release now
says that computer picked the chat up again, retaining the existing conflict
code and holder-changed detail. A different holder keeps the existing refusal;
neither refusal takes the chat.
