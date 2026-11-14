---
kind: changed
title: Optional idle handoff releases paired idle chats and opens free chats here
pr: 2287
surface: [chat, engine, app, remote, docs]
invalidates:
  - "An open idle chat previously kept renewing its computer's hold. That remains the default (`idle handoff` is off by default); with `idle handoff` (`CODEAF_IDLE_HANDOFF`) turned on in a paired set, an idle chat releases its hold after all saved state has reached the sync service."
  - "Selecting a released remote chat previously required Continue here. That remains the default (`idle handoff` is off by default); with `idle handoff` turned on, paired opens acquire a released chat automatically on terminal Home and through agents.open (`can_open`) without displacing a live holder."
  - "With `idle handoff` on (off by default), the first Home open of a local copy after restart could bypass free acquisition while the shared listing caught up. Ambiguous paired readings with `idle handoff` on now get a short ownership check, with local opening preserved on failure."
  - "With `idle handoff` on (off by default), every paired local Home open briefly required a successful sync-service read. Answered display rows now open without that read, and an ambiguous reading waits at most two seconds before falling back locally."
  - "With `idle handoff` on (off by default), a listed free local chat could still require a reachable sync service to open. Home now opens the saved local copy when the listing or free-open ownership read is unreachable; remote-only chats explain the failure."
  - "A desktop take previously blamed another device for every ownership conflict. A newer hold by the same computer now says that computer picked the chat up again."
  - "Automatic open with `idle handoff` on (off by default) briefly included lapsed holds. Lapsed holds retain Continue here whether `idle handoff` is off or on, including a holder last known to be working; an absent working advertisement cannot reliably prove idle."
  - "Opening a chat could claim it was asking again before checking whether a reply was owed. Refused owed replies now keep their question and show only the refusal."
  - "A released window's offline send could report that the chat was available without explaining why it could not send. It now names the unreachable sync service and keeps the words."
  - "With `idle handoff` on (off by default), an idle release attempted while the sync service was down could leave a live chat unable to send after a short outage. A release that never reached the service is now undone, and the chat sends on its own hold."
  - "After an outage in which this computer's own hold lapsed, the chat's window could refuse every Enter until restart. The first Enter now takes it back into a fresh writer at a new fence, with no card, when no other computer holds it."
  - "A send refused by the engine cleared the message box. The refused words now go back into the box."
---

The new paired idle-release and automatic-open behaviours are controlled by the
`idle handoff` setting (`idle_handoff` in `settings.json`, `CODEAF_IDLE_HANDOFF`
in the environment) and are **off by default**. With `idle handoff` unset or off,
paired computers behave as before: idle chats keep renewing their hold, and a
released remote chat still requires `Continue here`.

When `idle handoff` is turned on, running turns, queued or steering work, tasks,
jobs, deferred writes and unsent turns retain the hold. Task startup and parked
queued tasks retain it too. Saved settings and workspace edits made after the
last seal are captured and uploaded before release. Failed uploads retain the
hold. Idle preparation is canceled by new work and does not extend its ownership
deadline. With sync storage off (`StorageAllowed() == false`), idle release and
automatic open remain off even if `idle handoff` is enabled. The original window
stays open as a reader and reacquires before accepting new work. If another
computer won the hold, it refuses without recording the work and names the new
holder. Automatic open restores through the existing transfer path with
conditional, non-forcing acquisition. Older builds' live holds still require
Continue here.

Pairing is learned from existing device roster reads and watch events. Sync off,
unpaired and single-computer homes add no idle-release network requests. When
`idle handoff` is on, the app engine exposes available shared chats (`can_open`)
through its open door; desktop clients that only preview them need to call that
door to use automatic opening.

Ordinary messages and `!` shell commands in the original released window
reacquire in place, preserving local ignored install folders without a restore or
setup card. Opening a free chat refreshes an already-open stale viewer too.
Repeated moves keep an existing local root and use bounded, stable copy-folder
names on first arrival. When `idle handoff` is on and this window has no open
copy and Home has no reading for a local chat or still shows this computer's old
live hold, a paired home checks ownership for up to two seconds. A failed or
timed-out check opens the local chat as usual. Answered display rows need no
extra read, including a listed free chat or a chat already open in this window.
With `idle handoff` on, released paired chats restore the latest head through
conditional automatic acquisition at a new fence, including the first open after
a restart. Unpaired, sync-off, single-device and default (`idle handoff` off)
homes add no ownership read for local opening.

A lapsed hold on another computer retains the Continue-here card on terminal
Home and the app's chat row. Only explicitly released holds when `idle handoff`
is on advertise `can_open` or use automatic acquisition. This build keeps
consent for all lapsed holds: the optional working advertisement cannot
distinguish idle from an older holder that never advertised its working state. A
confirmed continuation still acquires conditionally at a new fence; stale
publishes cannot move that head, and the former holder keeps its local turns
separately.

Opening a conversation that owes no stopped reply never checks resume work
admission or prints the asking-again line. An owed reply refused by admission
shows the refusal once without claiming to resume, and retains the question.
A new send (plain message or `!` shell command) whose ownership read cannot
reach sync says `the sync service is not answering, so this chat cannot be taken
back yet; your words are still in the box` (or names the unreachable service).
It records nothing and calls no model or shell; the same words stay in the box
and can be retried at a new fence when the service returns.

When `idle handoff` is on, an unreachable Home listing opens saved local chats
directly, including this computer's released chats. A free take's initial
directory read has the same two-second limit; an unreachable read falls back to
the local copy, or keeps the existing unreachable refusal for a remote-only row.
Successful transfers retain their normal move budget and local opening keeps
existing send admission.

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

A lapsed or outside-released hold on this computer never resumes in place when
idle: its fence stops renewal beats and new work once the lapse is observed, and
the next Enter (including after an earlier displaced turn was saved to a kept
branch) makes a conditional take on the original chat, expecting the holder and
fence it observed, into a fresh writer that sends the kept words while the kept
branch remains a separate copy. Unpublished turns carry over or branch exactly
as an ordinary take does. A window that released its own hold still reacquires
in place. Another computer's hold, or any newer fence, keeps the Continue-here
card.

A turn that is already running when its own lease is observed to have lapsed
attempts a conditional in-place reacquire at a new fence when no other computer
holds the chat; on success it clears the outage notice and continues on the
original chat without creating a copy branch, and never renews a lapsed lease by
publishing. When another computer has taken the chat, running calls finish into
local history and go to a kept branch; an upload never overwrites a newer holder
and never reopens new work. Enter after recovery checks again even when Home
still shows the outage, or a failed move to another chat left this one marked,
and the box stays intact until the send succeeds.
