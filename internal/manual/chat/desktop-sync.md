# Sync in the desktop app

## Why did this desktop chat stop taking messages after another computer continued it

When another computer continues a chat, the desktop shows the engine's sentence:
`<name> moved this chat. This copy is read-only and may be behind`. This computer stops
accepting new messages, corrections, queued messages and retries for that chat.
Task and program starts, quick-task promotion, moving a reply to a task, task
directions and asking for a new crew proposal also refuse the move.
The same check protects remote submissions and queued follow-ups; a follow-up
accepted before the move is refused before its turn can start. Existing action
receipts remain readable and replaying one does not start its work again.
With remote control on and the other computer online, the desktop does not leave
you on that read-only copy: the same chat opens as a live view of the computer
that continued it, where you can read the running reply and type. The read-only
copy is what remains when remote control is off or that computer is off.
Your unsent draft and staged attachments stay yours. Messages already queued
stay in the queue; editing, reordering or removing them does not start a turn.
The app pins `Continued on <computer>` above the transcript with
`This copy is read-only and may be behind.` Its tab, sidebar and header use a
computer indicator. Move here replaces an inert composer; a composer that
can send to the other computer names that destination. Hidden drafts stay kept.
With a compatible engine, the open view follows newly saved messages through
hosted storage or live transfer, without moving the chat or restoring remote files.
If the latest history is unavailable, you can still read the local copy. Remote
history and previews read saved transcripts up to 24 MiB. For a larger transcript,
use Move here to move the chat and read its full history. Unsealed
words and in-progress tokens are not streamed between computers, except in a
live view (*Open a chat running on another computer live in the desktop*),
which shows the reply as the other computer writes it.
The desktop can still send that computer a message for the chat while it is
open or working there; it waits in the chat's queue on that computer. See
"Send a message to a chat working on another computer".

Sync news appears as visible notices in the chat: unsaved edits kept separately,
full sync space, a removed device, identity changes, clock problems, slowing,
too many new identities and trouble recording which turns reached other devices.
These notices are local to the open app. Reopening the history need not repeat
them; the chat's current location remains on its agent record while the engine
runs and is learned again when its sync drive starts after a restart.

## Can I answer a question after another computer continued the chat

Yes. Answers to questions already asked remain accepted in the desktop and
terminal: permissions, choices, proposals, intake forms, clarifications,
replacement answers and `You decide`. The turn can continue on your answer;
its tools still refuse to act here after the chat moved. A new message or retry
still refuses. Asking back about an open choice reaches that chat's model and
leaves the choice open. An ask-back or `You decide` accepted while the final
reply is arriving still reaches the model after that reply finishes.
Changing a crew member's brief updates crew state without opening its chat;
any resulting wake meets the session's ownership check.

Admission reads the location and viewer facts already known here. It does not
start or wait for sync startup, saving files or a relay read, and does not open
an unopened chat. A joined session host is asked for its cached fact remotely.

## When is the hosted sync disclosure counted as shown in the desktop

A home that never paired stays quiet and sends no relay requests. Pairing enables
the same per-chat sync drives used by the terminal. `CODEAF_SYNC_URL=off` keeps
sync off even after pairing. An incompatible engine says
`moving chats needs a newer furrow engine than this build carries`.

When a chat first starts syncing through the hosted service, the engine offers
its disclosure sentence. The desktop must show it before acknowledging it.
Reading sync status does not count as showing it. If the desktop never
acknowledges, a later desktop or terminal still owes the sentence. The terminal
continues to count it when it says it.

## Which desktop provider keys sync between paired computers

With relay storage enabled, saved keys for supported model providers can sync
through the encrypted vault. Connections that can point to different servers
on different computers, such as custom endpoints and Ollama, keep their keys
on the computer where you entered them. This includes an OpenRouter connection
redirected with `CODEAF_BASE_URL`. The desktop starts syncing provider keys
after reading its secure store, and uses a received key once that store saves it.
If you save or disconnect a key before sync can run, the change stays pending
on this computer across a restart until it can be captured in the encrypted vault.

## Can another terminal window take a chat the desktop holds

Yes. A local terminal's move request makes the app let the conversation go,
including while a reply runs. The chat says `moved to another window`. Work is
stopped with the same takeover cause the terminal uses, preserving the journal
and paused work for the window taking it. Later opens or sends from this app
engine answer the existing refusal about the conversation being open in another
codeaf window.

## Which commands were interrupted when codeaf was killed

When the app opens a chat with unfinished command records, each command is named
in a visible notice:
`<tool[: brief]> — stopped when codeaf was killed — not run again. Ask me to run it again if you still want it.`
The warning does not run the command again or claim that it succeeded.


## Relay in desktop Settings and Devices — address, default, off and pairing

With a compatible engine, Settings → Devices shows Relay and where its address
came from: set here, from pairing, default or off. An address set here wins over
one saved by pairing. Clearing it uses the paired address, or the hosted default
when none was saved. Off stops sync. Saving a desktop relay setting restarts its
engine; working agents require confirmation first. Cancelling keeps the setting.
A computer that has never paired remains quiet even with a relay address set.

## Pair a computer or enter a pairing code in Computers

Computers offers Pair a computer. Choose to show a code, or choose
Enter a code from the other computer to type one. Setup also offers joining.
A link can request or approve pairing instead.
Compare the engine's three words or check number on both computers, then answer
its explicit question. Nothing is approved by pressing Enter without a choice.
Choose Pair or Don't pair. The code's instruction says: `On your other computer,
choose Pair a computer, then Enter a code from the other computer, and type
<code> (dashes optional). This code lasts 10 minutes.` The approval names the other computer and
its platform, for example `Pair with Abir (Windows)?`, including when both
computers share a name.
Cancel takes back the waiting code or ends the request. A wrong code shows the
engine's sentence and does not add a computer.

A computer with submitted chats and its own pairing keys refuses ordinary
pairing, in the app's words: `This computer has 3 chats of its own, so it can't
join another computer's chats. Nothing was changed.` One already paired with
other computers is refused too: `This computer is already paired with <name>.`
Both refusals usually come before the other computer is asked, and its code
keeps waiting. When the refusal only comes after you chose Pair there, or the
other computer's confirmation is lost, the computer that showed the code does
not say they are paired: it says `Pairing was not confirmed by <name>. Check
that computer to see whether it joined.` For chats of its own (not for a
computer paired elsewhere), `codeaf pair <code> --replace` in a terminal joins
the other set instead: local chats stay here but stop syncing, shared memories
and settings clear, and local provider keys stay here. Use Move here to move a shared chat (or, when **Settings → General → idle handoff** / `CODEAF_IDLE_HANDOFF=1` is turned on — off by default — a shared chat no computer holds advertises `can_open` and opens directly through `agents.open` with no Move-here step; use **Move here** when another computer still holds it). A failed
upload stays saved locally and is retried while codeaf is open or on the next
start. Sync status says `Sharing this computer's chats.` during the pass and
`Some chats could not be shared yet. They are still saved here; codeaf will try
again.` on failure, including for unopened chats. A refused pairing changes nothing.
Finish any live work before joining another computer. After joining, the desktop
restarts its engine and reads setup, devices, sync and the shared chat list again.
These actions appear only when the engine advertises pairing support.

## When does a paired computer appear and when is its new name shared

Every paired desktop engine shares its own computer name and capabilities at
startup and after pairing, even with no chats. A failed write retries
automatically, starting after five seconds and doubling to a maximum of sixty
seconds between attempts. Success ends the retry. Reconnecting checks the
record again; an unchanged record is not rewritten. Sync off and a home that
has never paired remain quiet.

Renaming shares the new name immediately and says `This computer is now called
<name>.` If the relay cannot accept it yet, the name stays saved here and the
line adds `Your other computers will see it once this computer is back online.`
The engine retries without another rename or a chat.

Computers lists this computer and the relay's roster even while this computer's
own record is still waiting to be shared. A separate presence failure leaves
online status unknown; a readable roster is still shown. Changes arrive through
engine events, and unchanged lists do not repeat them.

## Chats on your other computers and Move here in the desktop

Chats from other computers appear under On your other computers, with the
computer's name. An online dot appears only when the engine knows its presence.
Finished movable chats saved before pairing are shared after pairing and when
the paired app engine starts. This does not open the chat or start a new turn.
An open, idle chat another computer still holds says `open on <name>` (`held on <name>` in the terminal); this says where it is open, not that a reply is running. By default (`idle handoff` is off by default; turn it on in **Settings → General → idle handoff** or with `CODEAF_IDLE_HANDOFF=1`), an open idle chat keeps its hold; with `idle handoff` on in paired builds, an idle chat lets go about 2 seconds after going idle once saved to the relay, so rows stay `open on <name>` only before release, while unsaved work keeps the hold, when `idle handoff` is off, or on older builds. While a reply runs, the engine advertises `working: true` (`working on <name>`), including right after a move, and clears it once the reply finishes and its last saved turn reaches the relay. Closing a chat does not make its computer offline; `<name> offline` appears only when that computer is known to be offline, and unknown presence omits both dot and offline line. The saved time is the newest shared saved turn's time in your local time, not upload time, and is omitted when unknown. The engine's takeover sentence adds holder or pending-work facts without repeating a date or age. After the relay starts or restarts, computers it has not heard from have unknown presence (no offline line) for up to 75 seconds; an observed connection that lapses can still be marked offline.
With `idle handoff` on, a shared chat explicitly released by its holder advertises `can_open: true` and `agents.open` pulls and opens it directly with no Move-here step (while `idle handoff` is off, `can_open` is not advertised and released chats use Move here). A lapsed hold never advertises `can_open`, including when its holder was working or its last working state is unknown. Select a held or lapsed chat (or use a preview-only client, or when `idle handoff` is off) to see its title, saved time and engine sentence: **Move here** moves it through the engine and opens the returned local chat, using the same continuation path as the terminal.

## Moving a desktop chat here — fences, retries and local copies

An already local chat that moved away keeps its place and becomes read-only.
Move here is available when the engine says it can move. Moving a chat here
stops new work on the other computer. If another computer moved it after the
preview was read, the engine says `another device moved this chat first`;
refresh the row and choose again. Only a Move here that waited for a turn retries itself. Unsent drafts
and attachments remain yours. Changes arrive as engine events without desktop
polling. An older chat that works in the home folder stays on that computer.

Move here requires the holder and ownership fence shown when you chose it.
A changed fence, holder or saved turn needs a fresh choice.
Completed turns are shared while the chat stays open, including after a move.

## Open a chat running on another computer live in the desktop — Open on <name>, watch it, type into it, take the keyboard

With **Remote control** on (the **Remote control** switch in Settings → Devices, off by default; *Turn remote control on or off in the desktop app*) the desktop can open a chat another of your computers is running as a live view. A chat row that is running on a computer that is online offers `Open on <name>`; choosing it opens the chat like any other: its history, the reply as it streams, questions waiting for you, steer, queue and stop all work, and you type in the ordinary box (`Message <name>`). The words go straight to the running chat on that computer. Nothing is kept on the service in between, so this works with storage off, and it is not the waiting message of *Send a message to a chat working on another computer*, which is only for a computer that is off. The chat stays on that computer; `Move here` brings it here.

The chat's strip says where it stands: `live` (with `· direct` or `· relay` after it once the road is known, as `/status` says in the terminal), `reconnecting`, `offline` with when it was last seen, `idle`, or `watching`. A view with no one typing for ten minutes disconnects itself so an idle view costs nothing; sending anything, or opening the chat again, reconnects. Watching means that computer lets this one read but not type (`<name> lets this computer watch, not type. Change that in /devices there.`), or its daily allowance of live traffic is used up (`watch only until 00:00 UTC (daily limit)`). Only one computer types at a time: `typing from <name> now` says who has the keyboard, and taking it is the button beside it. A computer taken off your list is told so (`this computer was removed from your devices`).

The model, approval posture and spending limit of a live view are set on the computer running the chat, not from here, and the desktop's own tools are not given to that chat.

What a computer lets another one do is set on that computer, in Settings → Devices → each other computer: watch its chats, and type into them. A computer paired before remote control existed can watch but not type until you allow it; one paired since can do both, and you can turn either off. Chats a terminal holds can be opened live from the desktop, and a chat this desktop is running can be opened live from your other computers (*Offer a chat running on this desktop to another computer*).

## Turn remote control on or off in the desktop app — the Remote control switch, where it is, why it says restart

In the desktop app, open Settings → Devices: the **Remote control** switch is at the top, above Computers. It is off until you turn it on, and it is this computer's own setting, the same one the terminal's `/settings` → `Devices` row changes. If the app was started with `CODEAF_REMOTE_CONTROL` set, the switch shows that value and cannot be changed; it says `Remote control is set by CODEAF_REMOTE_CONTROL`. A desktop app older than remote control has no such switch.

Turning it **on** offers the chats this desktop opens from then on to your other computers. A chat that was already open is not offered until you open it again. The `Open on <name>` buttons and each computer's watch and type switches appear after the app's engine restarts, so the switch says `Restart to open chats on your other computers.` with a **Restart** button; if an agent is working it asks first.

Turning it **off** takes effect at once: the desktop stops offering chats, and another computer's view of one of them ends with `<this computer> closed this chat.`. The chat here keeps running and keeps everything that was typed. The `Open on` buttons go away after a restart (`Restart to take the Open on buttons away.`).

## Offer a chat running on this desktop to another computer — remote control, who may open it, how long, when it ends

With **Remote control** on (the **Remote control** switch in Settings → Devices, off by default), a chat this desktop is running can be opened live from your other computers, the way the desktop opens theirs: they see the history and the reply as it streams, and write in it only if this computer lets them. That is set the same way as the other direction, in Settings → Devices → that computer: whether it may watch your chats here, and whether it may write in them (a computer paired before remote control existed starts with watching only). The desktop's own window stays in the chat the whole time and keeps the chat's words; another computer is one more window on the same conversation, not a copy.

A chat is offered while this desktop has it open. One a window has left idle is let go after its usual idle time, **unless another computer is watching it**, which keeps it open until they leave. Closing the chat here or quitting the desktop ends their view: they see `<this computer> closed this chat.`, and the chat reads from the saved copy from then on. A chat opened while remote control was off is not offered; turn it on, and chats opened afterwards are.

A running factory stage is not offered. With remote control off, or when the desktop cannot open the door another computer would knock on (the state folder's path is too long for a socket, or its folder is read-only), nothing here applies and the chat says where it is held, as before.

## Who is typing in a chat this desktop is running — Typing from another computer, Type here, take the keyboard back

With remote control on, another of your computers (or a second window on this one) can take the keyboard of a chat this desktop is running, and only one window types at a time. The desktop's own box is then read only and the chat says `Typing from <that computer> now.` (`another window` when it is a second window here) with a **Type here** button that takes the keyboard back. A message sent while the keyboard is theirs is refused with the sentence that says who has it. The keyboard comes back to the desktop when they let go or leave, and the chat keeps running either way. With remote control off nobody else is in the chat, so nothing is shown and **Type here** never appears.

## A crew made on another computer in the desktop — grouped, read-only, message its manager

A crew made on another paired computer shows in the desktop as one crew: its
manager and members are grouped under the crew's name, not listed as loose
chats. Factory crews from another computer group the same way. The grouping
is what that computer said at each chat's last saved turn, so it stays while
that computer is off; a rename or a member leaving there shows once that chat
saves its next turn. A chat from a computer on an older codeaf, or through a
relay that does not carry crews yet, shows as a loose chat.

In this version a crew on another computer is read-only here:

- You can read each member's chat and send a message to the crew's manager,
  which is queued on that computer and runs after its turn.
- The crew is not in this computer's crews list, cannot be edited, closed or
  started here, and cannot be moved here as a crew. Move here still
  moves one member's chat on its own, which leaves the crew on the other
  computer.

A crew made on this computer is grouped the same way, from this computer's own
crews.

## Send a message to a chat working on another computer — queued there, runs after the turn

While another computer still holds a chat (working, or open while `idle handoff` is off, before idle release, or on an older build), the desktop composer
writes to that computer instead of being read-only. The field says
`Message <name>`, and where the model picker usually is, a chip beside Send
says where it goes: `To <name> · after this turn` while a turn runs there,
`To <name>` while the chat is only held open. When `idle handoff` (`CODEAF_IDLE_HANDOFF=1`, off by default) is on and an idle chat lets go of its hold, no computer holds it: opening it via `agents.open` pulls it here directly, and sending in the window that released it takes it back in place. Rest on the chip or press it to see
which computer it is (`<name> · your Linux computer`), what happens to a
message, and `Move here instead`. The chat does not move. The
message goes into the chat's queue on that computer, so it runs when the
current turn ends, or at once if nothing is running.

The sent message shows in the chat with one dim line: `Sending to <name>`
until that computer puts it in the queue, then `Delivered to <name>`. Once the
chat comes here its transcript shows what ran, and those lines go.

The chat is the address, not the computer: if the chat moves before the message
is taken, whichever computer holds it then takes it, which can be this one. So
moving a chat here runs your messages still waiting for the other computer
on this computer, right after the move; the chat says so beside every Move here, with the count of waiting messages
that will run on this computer. A waiting message cannot be taken back.

To bring the chat here instead, choose Move here. While a turn runs there
it waits (`Comes here when this turn ends.`, with Cancel), then moves, retrying
once if that computer saved again first.

## Why can't I message a chat on another computer — the composer is read-only while relay storage is off

While the account's hosted relay storage is off, the desktop does not offer a *waiting* `Message <name>` for a chat another computer holds, and `chats.send` is not in `hello`. A chat that computer is running and that you opened live is different: with remote control on and the computer online, you type into it directly and storage is not involved (*Open a chat running on another computer live*). The rest of this section is about a computer that is off, or a chat opened without remote control. A waiting message is sealed text kept in this computer's record on the service until the holder takes it, and the service keeps nothing of the kind until storage is approved. A send that arrives anyway is refused with `Relay storage is off for this account, so a message can't wait for another computer. Move the chat here to write to it.` and leaves nothing on the service.

Choose Move here to write to the chat: blind pass-through moves it while both computers run codeaf. Approving relay storage brings the message box back on the next connection, and a self-hosted relay always had it. Messages already waiting when storage was switched off can still be taken off the service.

## A message to another computer is not delivered, the computer is offline, or it didn't send

These are messages written here to a chat that another of your computers holds.

If the other computer goes offline before it takes a message, the message's
line reads `Not delivered yet · <name> is offline`, and the chat says
`Your 2 messages waiting for <name> go when it's back online, or run here if you move here.`
The text is sealed with your identity's key, so the relay cannot read it.

Limits and refusals:

- At most eight messages from this computer wait at once, counted across every chat and every other computer, not per chat: `Eight messages sent from this computer, in this chat or others, haven't been delivered yet. Send this one once some of them are.`
- When this computer cannot reach your relay, nothing is sent and the words stay in the box: `This computer can't reach your relay right now, so the message didn't go. Send it again once it's back.` The box says `Can't reach your relay · messages to <name> send once it answers` while that lasts.
- A message is at most 2000 characters: `Messages to another computer are limited to 2000 characters.`
- A relay that does not carry messages says `This relay can't carry a message to another computer yet. Move the chat here to write to it.`
- Only an app engine (the desktop app, or `codeaf app`) on the holding computer takes messages. A terminal chat there does not; the message waits.
- A chat whose holding computer is offline with a live hold or a lapsed hold stays read-only until you move it here, including when its holder was working. With `idle handoff` on (`CODEAF_IDLE_HANDOFF=1`, off by default), an explicitly released chat opens here directly through `agents.open` on `can_open` rows, and new work in the window that released its own idle hold takes it back in place (with `idle handoff` off, a handed-over chat's next message moves it here via Move here).

## Does an idle desktop chat keep its hold on the relay

By default (**Settings → General → idle handoff** is off), a chat opened for viewing keeps its conversation and sync-service hold until it is explicitly closed, including while its tab is hidden; heartbeats renew the same hold without releasing it or moving its fence, and reading the chat does not cause ownership changes or repeat unchanged chat rows.

When you turn **idle handoff** on in `/settings` or with `CODEAF_IDLE_HANDOFF=1` with two or more paired computers (and relay storage on), an idle chat lets go of its hold about 2 seconds after it goes idle once its journal, working files, shared memory and settings are on the relay, including when its tab stays open. The conversation remains open as a reader. Running turns, queued or steering work, running tasks or jobs, deferred writes and failed uploads retain the hold. Rebuildable files keep their existing transfer exclusions.

New work in the window that released its idle hold takes the unchanged chat back in place, with no prompt, before recording it or calling a model. If another computer took it, the existing moved notice and held-elsewhere refusal name that computer. A hold that merely lapsed (not released by its window) admits nothing until you reopen or continue it. A released window stays read-only when ownership cannot be confirmed. Unpaired, sync-off, single-device and default (`idle handoff` off) homes keep their existing hold and renewal behavior.

With `idle handoff` on, through `agents.open` and `can_open` rows, the engine opens an explicitly released shared chat on another paired computer directly with no Move-here step. A lapsed hold keeps Move here, including when its holder was working or its last working state is unknown. App versions that only preview shared chats (or homes with `idle handoff` off) still offer Move here in that preview. A chat another computer still holds (`held on <device>` / `working on <device>`, including idle chats when `idle handoff` is off or on older builds) continues to ask with **Move here** as your choice.

## Background desktop conversations release after ten idle minutes

A conversation opened only for background work is let go after ten idle minutes;
the sweep checks every 30 seconds. It stays released until work needs it again.
Sending, queueing or retrying opens it through the normal ownership check.
Sending into your own released chat opens it again when its hold is still free;
its own idle release does not require Move here.
Steering still needs a running turn. A task or job can also open it. Running
turns, unanswered questions, held crew members and kept services retain their
conversations. If another computer moves a chat, the existing Move here
and kept-work rules still apply.

Stopped processes, finished tasks and settled jobs do not reopen a background
chat at startup or after release. Recovery repairs a stale running process
record when the matching task checkpoint already knows it ended. Unfinished
work still recovers normally. Releasing the last background chat does not restart
sync or repeat settled preferences.

## Relay outages, local turns and automatic recovery

A refused network connection or an unavailable upload service produces one
outage notice in the affected chat. A successful publication reports recovery.
A local save alone does not clear it. Storage, quota and rate limits have their
own notices. If storage is turned off while you work, a successful quit records
that the final turns stayed here in the desktop engine log.

While the relay is unreachable, an open chat continues to run and save on this
computer. Unpublished turns retry automatically with waits capped at 60 seconds, without
another send, quit or restart. Once an ownership check finds the hold expired,
an idle window stops admitting work. In an open terminal chat, the next Enter
conditionally reopens it at a new fence and sends the kept draft without a
Move here card. A desktop can use Move here to acquire a new hold.
Reopening a chat also acquires a new ownership fence. If this computer's renewal
or the relay's renewal at restart reaches the unchanged hold before an ownership
check observes expiry, the open chat keeps its fence and uploads its local turns.

A turn that is already running when the hold is observed to have expired attempts a conditional in-place reacquire at a new fence when no other computer holds the chat, continuing on the original chat without a copy branch; if another computer holds the chat, finished calls are kept as a branch. An upload never overwrites another computer's newer hold and never renews a lapsed lease by publishing. If the service is down, those saved turns remain local and retry when it returns.

If another computer continued the chat meanwhile, this window becomes read-only
and names that computer. Work saved here during the outage is kept through the
usual kept-work path. Use Move here to bring the current shared chat back.

## Move here when this computer cannot use the chat's model

A chat saved on another computer can name a provider that is not set up here.
When it opens here, codeaf checks the same local provider readiness used for
shared model settings. An unavailable model continues with this computer's usable
default. One line in the chat explains which model was unavailable and which
model is used here. The existing conversation model-switch rules save that choice;
continuing back on a computer without that provider checks its own default again.
Every continuation uses the saved chat's latest main and fallback choices,
including when this computer already has an older copy. A usable saved model
stays selected without a change notice. An unavailable main model gets one
specific explanation; a separate fallback change may have its own line.
Move here completes even when the default is also unavailable: the chat is
here, and its row opens the returned local agent. Its first send fails locally
with a setup sentence, such as `This chat used llama3.2 (Ollama), which isn't set up on this computer. Set up a model in Settings, then send again.`
That reason appears once in the agent view, and no provider call is made for
the turn. Set a usable default in Settings, then send again. The next send uses
that default and explains the change once; taking the chat again is unnecessary.

## If Move here fails while moving a chat

If moving the chat fails, the app says
`This chat could not be moved to this computer.` Technical causes are kept in
the error details. A plain model setup reason is carried in the visible message.
If installation failed after acquisition, the chat is released.
The computer without the installed chat can choose Move here (or, with `idle handoff` on, open the now-free row directly through `agents.open` / `can_open`), and the row names the
last holder without claiming it is open there. A window that was displaced by that move attempt says
`This chat is available to move here.` Because another computer's move displaced it rather than its own idle release, it admits nothing until you reopen or move it here.
If another computer saved newer work, reopening or Move here restores that work.

## Shared settings after pairing — model, effort and spending limits

When shared preferences arrive from another paired computer, desktop Settings
updates through the engine's settings event. One restore reports the full current
settings once, including changes to the default model, effort and spending limits.
An unchanged restore sends no settings event. Repeated roster notifications also
send no Devices event when the displayed computer list has not changed.
A model choice is applied here only when its provider is set up on this computer;
otherwise this computer keeps its own model. Setting up that provider later does
not apply an older skipped choice; only a newer choice on the other computer travels.

## Why the desktop takes a moment to start after an update or a quick reopen

One desktop engine runs on your codeaf home at a time, so two can't spend on the same work. If the last one is still saving as it closes, the new one waits, starts nothing, and carries on as soon as the last one ends. A crash doesn't make you wait.

If the last one is still there after about two minutes, the new one stops and the log says `another codeaf app is still closing this home`. Quit it, then open the app again. Terminal `codeaf` and headless commands can use other homes. If you open WorkAF again with a different home while it is already running, it asks you to quit the running WorkAF first.

## Saved-key sync when the local vault is unreadable

A removal check returns a local vault read error rather than stopping the
background sync worker. Background sync can retry on a later round. This does
not repair a damaged vault or change the saved keys shared between computers.

## Move here while a background job runs on the other computer

Move here waits until that computer's foreground turn, background jobs and
resulting replies are finished and saved. The notice says
`Comes here after its running work and replies are saved.` Cancel stops waiting.
Job completion and its owed reply stay on the original computer until that
boundary; the moved chat then carries the real ending, elapsed time and output within
the existing file carry limits. Its process history and managed output remain
readable on the receiving computer; an output read waits for arriving files.
A long-running job can keep the move waiting; cancelling does not stop the job.

The waiting view follows saved remote messages through the existing directory
watch. This is saved history, not a live stream of unsaved text. Older engines
without `chats.read` retain their previous local-history behavior.

## Where did the received repository land after a chat move

A received working copy shows `Working copy on this computer`, its actual folder
and `Copied from <original folder>`. Show folder reveals that working directory.
The private copy stays in codeaf's state folder. Commands use the local path shown
in that notice; the source path identifies the original copy.
