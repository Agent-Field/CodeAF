# Sync in the desktop app

## Why did this desktop chat stop taking messages after another computer continued it

When another computer continues a chat, the desktop shows the engine's sentence:
`<name> continued this chat; this window now only shows it`. This computer stops
accepting new messages, corrections, queued messages and retries for that chat.
Task and program starts, quick-task promotion, moving a reply to a task, task
directions and asking for a new crew proposal also refuse the move.
The same check protects remote submissions and queued follow-ups; a follow-up
accepted before the move is refused before its turn can start. Existing action
receipts remain readable and replaying one does not start its work again.
Your unsent draft and staged attachments stay yours. Messages already queued
stay in the queue; editing, reordering or removing them does not start a turn.
You can still read the chat's history and files.
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
pairing and explains replacement. Confirm `--replace` to join the other set:
local chats stay here but stop syncing, shared memories and settings clear,
and local provider keys stay here. On paired computers, a shared chat no computer holds (`can_open`) opens directly through `agents.open` with no Continue-here step; use **Move here** or **Continue here** when another computer still holds it. A failed
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

## Chats on your other computers and Continue here in the desktop

Chats from other computers appear under On your other computers, with the
computer's name. An online dot appears only when the engine knows its presence.
Finished movable chats saved before pairing are shared after pairing and when
the paired app engine starts. This does not open the chat or start a new turn.
An open, idle chat another computer still holds says `open on <name>` (`held on <name>` in the terminal); this says where it is open, not that a reply is running. On current paired builds an idle chat lets go about 2 seconds after going idle once saved to the sync service, so rows stay `open on <name>` only before release, while unsaved work keeps the hold, or on older builds. While a reply runs, the engine advertises `working: true` (`working on <name>`), including right after a move, and clears it once the reply finishes and its last saved turn reaches the sync service. Closing a chat does not make its computer offline; `<name> offline` appears only when that computer is known to be offline, and unknown presence omits both dot and offline line. The saved time is the newest shared saved turn's time in your local time, not upload time, and is omitted when unknown. The engine's takeover sentence adds holder or pending-work facts without repeating a date or age. After the sync service starts or restarts, computers it has not heard from have unknown presence (no offline line) for up to 75 seconds; an observed connection that lapses can still be marked offline.
For a shared chat explicitly released by its holder, its row advertises `can_open: true` and `agents.open` pulls and opens it directly with no Continue-here step. A lapsed hold does not advertise `can_open`, including when its holder was working or its last working state is unknown. Select a held or lapsed chat (or use a preview-only client) to see its title, saved time and engine sentence: **Move here** or **Continue here** moves it through the engine and opens the returned local chat, using the same continuation path as the terminal.

## Continuing a desktop chat here — fences, retries and local copies

An already local chat that moved away keeps its place and becomes read-only.
Continue here is available when the engine says it can move. Continuing here
stops new work on the other computer. If another computer moved it after the
preview was read, the engine says `another device continued this chat first`;
refresh the row and choose again. Only a Continue here that waited for a turn retries itself. Unsent drafts
and attachments remain yours. Changes arrive as engine events without desktop
polling. An older chat that works in the home folder stays on that computer.

Continue here requires the holder and ownership fence shown when you chose it.
A changed fence, holder or saved turn needs a fresh choice.
Completed turns are shared while the chat stays open, including after a move.

## Send a message to a chat working on another computer — queued there, runs after the turn

While another computer still holds a chat (working, or open before idle release or on an older build), the desktop composer
writes to that computer instead of being read-only. The field says
`Message <name>`, and where the model picker usually is, a chip beside Send
says where it goes: `To <name> · after this turn` while a turn runs there,
`To <name>` while the chat is only held open. Once an idle chat lets go of its hold, no computer holds it: opening it via `agents.open` pulls it here directly, and sending in the window that released it takes it back in place. Rest on the chip or press it to see
which computer it is (`<name> · your Linux computer`), what happens to a
message, and `Continue here instead`. The chat does not move. The
message goes into the chat's queue on that computer, so it runs when the
current turn ends, or at once if nothing is running.

The sent message shows in the chat with one dim line: `Sending to <name>`
until that computer puts it in the queue, then `Delivered to <name>`. Once the
chat comes here its transcript shows what ran, and those lines go.

The chat is the address, not the computer: if the chat moves before the message
is taken, whichever computer holds it then takes it, which can be this one. So
continuing a chat here runs your messages still waiting for the other computer
on this computer, right after the move; the chat says so beside every Continue
here (`Continuing here runs your 2 waiting messages on this computer.`). A
waiting message cannot be taken back.

To bring the chat here instead, choose Continue here. While a turn runs there
it waits (`Comes here when this turn ends.`, with Cancel), then moves, retrying
once if that computer saved again first.

## A message to another computer is not delivered, the computer is offline, or it didn't send

These are messages written here to a chat that another of your computers holds.

If the other computer goes offline before it takes a message, the message's
line reads `Not delivered yet · <name> is offline`, and the chat says
`Your 2 messages waiting for <name> go when it's back online, or run here if you continue here.`
The text is sealed with your identity's key, so the relay cannot read it.

Limits and refusals:

- At most eight messages from this computer wait at once, counted across every chat and every other computer, not per chat: `Eight messages sent from this computer, in this chat or others, haven't been delivered yet. Send this one once some of them are.`
- When this computer cannot reach your relay, nothing is sent and the words stay in the box: `This computer can't reach your relay right now, so the message didn't go. Send it again once it's back.` The box says `Can't reach your relay · messages to <name> send once it answers` while that lasts.
- A message is at most 2000 characters: `Messages to another computer are limited to 2000 characters.`
- A sync service that does not carry messages says `This sync service can't carry a message to another computer yet. Bring the chat here to write to it.`
- Only an app engine (the desktop app, or `codeaf app`) on the holding computer takes messages. A terminal chat there does not; the message waits.
- A chat whose holding computer is offline with a live hold or a lapsed hold stays read-only until you continue it here, including when its holder was working. An explicitly released chat opens here directly through `agents.open` on `can_open` rows, and new work in the window that released its own idle hold takes it back in place.

## Does an idle desktop chat keep its hold on the sync service

With two or more paired computers, an idle chat lets go of its hold about 2
seconds after it goes idle once its journal, working files, shared memory and
settings are on the sync service, including when its tab stays open. The
conversation remains open as a reader. Running turns, queued or steering work,
running tasks or jobs, deferred writes and failed uploads retain the hold.
Rebuildable files keep their existing transfer exclusions.

New work in the window that released its idle hold takes the unchanged chat
back in place, with no prompt, before recording it or calling a model. If
another computer took it, the existing moved notice and held-elsewhere refusal
name that computer. A hold that merely lapsed (not released by its window)
admits nothing until you continue it. A released window stays read-only when
ownership cannot be confirmed. Unpaired, sync-off and single-device homes keep
their existing hold and renewal behavior.

Through `agents.open` and `can_open` rows, the engine opens an explicitly
released shared chat on another paired computer directly with no Continue-here
step. A lapsed hold keeps Continue here, including when its holder was working
or its last working state is unknown. App versions that only preview shared chats still offer Continue here in
that preview. A chat another computer still holds (`held on <device>` /
`working on <device>`, including idle chats on older builds) continues to ask
with **Move here** or **Continue here** as your choice.

## Background desktop conversations release after ten idle minutes

A conversation opened only for background work is let go after ten idle minutes;
the sweep checks every 30 seconds. It stays released until work needs it again.
Sending, queueing or retrying opens it through the normal ownership check.
Steering still needs a running turn. A task or job can also open it. Running
turns, unanswered questions, held crew members and kept services retain their
conversations. If another computer continues a chat, the existing Continue here
and kept-work rules still apply.

Stopped processes, finished tasks and settled jobs do not reopen a background
chat at startup or after release. Recovery repairs a stale running process
record when the matching task checkpoint already knows it ended. Unfinished
work still recovers normally. Releasing the last background chat does not restart
sync or repeat settled preferences.

## Relay outages, local turns and automatic recovery

While the relay is unreachable, an open chat continues to run and save on this
computer. Unpublished turns retry automatically with waits capped at 60 seconds, without
another send, quit or restart. Once an ownership check finds the hold expired,
the window stops admitting work. Use Continue here to acquire a new hold.
Reopening a chat also acquires a new ownership fence.

If another computer continued the chat meanwhile, this window becomes read-only
and names that computer. Work saved here during the outage is kept through the
usual kept-work path. Use Continue here to bring the current shared chat back.

## Continue here when this computer cannot use the chat's model

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
Continue here completes even when the default is also unavailable: the chat is
here, and its row opens the returned local agent. Its first send fails locally
with a setup sentence, such as `This chat used llama3.2 (Ollama), which isn't set up on this computer. Set up a model in Settings, then send again.`
That reason appears once in the agent view, and no provider call is made for
the turn. Set a usable default in Settings, then send again. The next send uses
that default and explains the change once; taking the chat again is unnecessary.

## If Continue here fails while moving a chat

If moving the chat fails, the app says
`This chat could not be moved to this computer.` Technical causes are kept in
the error details. A plain model setup reason is carried in the visible message.
If installation failed after acquisition, the chat is released.
The computer without the installed chat can open the now-free row directly through `agents.open` (`can_open`) or choose Continue here, and the row names the
last holder without claiming it is open there. A window that was displaced by that move attempt says
`This chat is available to continue here.` Because another computer's move displaced it rather than its own idle release, it admits nothing until you reopen or continue it here.
If another computer saved newer work, reopening or Continue here restores that work.

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

If the last one is still there after about two minutes, the new one stops and the log says `another codeaf app is still closing this home`. Quit it, then open the app again. Terminal `codeaf`, headless runs and other `--home` folders don't wait.

## Saved-key sync when the local vault is unreadable

A removal check returns a local vault read error rather than stopping the
background sync worker. Background sync can retry on a later round. This does
not repair a damaged vault or change the saved keys shared between computers.
