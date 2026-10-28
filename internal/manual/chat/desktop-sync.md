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

## Add a computer or join another computer from the desktop

Devices offers Add a computer to show a code, and Join another computer to type
one. Setup also offers joining. A link can request or approve pairing instead.
Compare the engine's three words or check number on both computers, then answer
its explicit question. Nothing is approved by pressing Enter without a choice.
Choose Pair or Don't pair. The code lasts ten minutes; its instructions name
Join another computer on the other computer.
Cancel takes back the waiting code or ends the request. A wrong code shows the
engine's sentence and does not add a computer.

A computer that already has chats asks before replacing them. Choose the option
Not now to cancel without changing them, or Replace this computer's chats to
continue. Finish any live work before
joining another computer. After joining changes what this computer shares, the
desktop restarts its engine and reads setup, devices and sync again.
These actions appear only when the engine advertises pairing support.

## Chats on your other computers and Continue here in the desktop

Chats from other computers appear under On your other computers, with the
computer's name. An online dot appears only when the engine knows its presence.
Finished movable chats saved before pairing are shared after pairing and when
the paired app engine starts. This does not open the chat or start a new turn.
An open, idle chat says `open on <name>`; this describes where it is open and
does not mean a reply is running. Closing a chat does not make its computer
offline. `<name> offline` appears only when that computer is known to be offline;
unknown presence has no online dot or offline claim. The saved time is the time
of the newest shared saved turn, shown in your local time, rather than the time
it was copied to the relay. An unknown saved time is left out. The engine's
takeover sentence adds holder or pending-work facts without repeating a date or age.
After a relay starts or restarts, computers it has not heard from have unknown
presence for up to 75 seconds. They have no offline line during that window.
An observed computer whose connection actually lapses can still be marked offline.
Select a chat to preview its title, saved time and engine sentence. Continue
here moves it through the engine, then opens the returned local chat. Its
history and working copies use the same continuation path as the terminal.

An already local chat that moved away keeps its place and becomes read-only.
Continue here is available when the engine says it can move. Continuing here
stops new work on the other computer. If another computer moved it after the
preview was read, the engine says `another device continued this chat first`;
refresh the row and choose again. There is no automatic retry. Unsent drafts
and attachments remain yours. Changes arrive as engine events without desktop
polling. An older chat that works in the home folder stays on that computer.

Continue here still works after a reconnect when the same computer holds the
same saved turn. A different holder or a newer saved turn needs a fresh choice.
Completed turns are shared while the chat stays open, including after a move.

## Does an idle desktop chat keep its hold on the relay

A chat opened for viewing keeps its conversation and relay hold until it is
explicitly closed, including while its tab is hidden. Heartbeats renew the same
hold without releasing it or moving its fence. Reading the chat does not cause
ownership changes or repeat unchanged chat rows.

A conversation opened only for background work is let go after ten idle minutes;
the sweep checks every 30 seconds. It stays released until work needs it again.
Sending, queueing or retrying opens it through the normal ownership check.
Steering still needs a running turn. A task or job can also open it. Running
turns, unanswered questions, held crew members and kept services retain their
conversations. If another computer continues a chat, the existing Continue here
and kept-work rules still apply.

## Relay outages, local turns and automatic recovery

While the relay is unreachable, an open chat continues to run and save on this
computer. Unpublished turns retry automatically with waits capped at 60 seconds, without
another send, quit or restart. A lapsed hold that still belongs to this computer
is renewed when the relay returns; its saved work is then shared. The chat stays
writable and never names this computer as having continued it elsewhere.

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
The computer without the installed chat still offers Continue here and names the
last holder without claiming it is open there. The original open chat says
`This chat is available to continue here.` Its next send can acquire the free
lease when its saved head still matches the relay.
If another computer saved newer work, use Continue here to restore that work.

## Shared settings after pairing — model, effort and spending limits

When shared preferences arrive from another paired computer, desktop Settings
updates through the engine's settings event. One restore reports the full current
settings once, including changes to the default model, effort and spending limits.
An unchanged restore sends no settings event. Repeated roster notifications also
send no Devices event when the displayed computer list has not changed.
A model choice is applied here only when its provider is set up on this computer;
otherwise this computer keeps its own model. Setting up that provider later does
not apply an older skipped choice; only a newer choice on the other computer travels.
