---
kind: changed
title: Paired idle chats release their hold after upload and available chats open here automatically
pr: 2287
surface: [chat, engine, app, remote, docs]
invalidates:
  - "An open idle chat previously kept renewing its computer's hold. In a paired set it now releases after all saved state has reached the relay."
  - "Selecting a released or lapsed remote chat previously required Continue here. Paired terminal opens now acquire it automatically without displacing a live holder."
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
acquire. No directory protocol or replicated lease record changes are included.
