# Tasks on another computer

## Can a task run on my other computer

Yes. Pair both computers with the same identity and run `codeaf serve --take-tasks` on the
computer that will do the task. Both ends need Linux or macOS; Windows cannot
send or receive these tasks yet. An ordinary open chat window does not take tasks.
Only a task that works in its own copy can go.

Choose the computer with `/task --on <computer> <brief>` or **Run tasks on**
in `/settings` → Tasks. Until you choose, ordinary tasks run here without
contacting other task computers. A proposal already naming a computer, or
one made with a remote default, offers `ctrl+d computer`. The
model gets the optional `computer` argument only while **Run tasks on** names
a paired computer. Use its shown device name, not its connection name. The named choice wins over the setting; the default is
`this computer`. These controls are absent when paired tasks are unavailable.

The chat, conversation file and transcript stay here. The task's copy goes
encrypted to the other computer, and its change comes home through the ordinary
landing road, including its commits. With private task copies on, use
`/task review <number> work` then `/task apply <number> work` here instead.
That apply is the same one a task that ran here uses, with the same limits: on
a Mac it refuses a project whose files belong to a group other than yours,
saying `portable apply cannot preserve a different owner or group`.
The other computer uses its own provider keys; its spend appears here against
the conversation and task, once, and also stays on that computer's ledger.

## Run a task on my mac mini

Pair your Mac mini and this computer, then run `codeaf serve --take-tasks` on the mini.
Use its shown name, quoting names that contain spaces:

```
/task --on "mac mini" Write the export regression test
/task --cheap --on "mac mini" solo Fix the export
```

`--best` or `--cheap` may come before or after `--on`; `solo` follows those
flags. `here` and `this computer` choose this computer. A proposal's
`ctrl+d computer` cycles through `this computer` and eligible paired computers
over an empty draft; accepting the proposal sends that choice. The line says
`runs on <name>`. This line and cycle key are absent from ordinary local
proposals until the setting or proposal names a computer. The model's
`computer` argument is available only while **Run tasks on** names a paired
computer. Two computers with the same name use their Devices labels, such as
`mac mini #1111` and `mac mini #2222`. Include that label when choosing one.
An ambiguous bare name says
`"<name>" matches more than one paired computer — choose one of: <names>`.
A misspelt name is refused with
`no paired computer is named "<name>" — your computers are: <names>`.

Both computers must run Linux or macOS, and the task needs its own copy. Sending
a task does not move the chat or transcript. The mini uses its own provider keys.

## What does running on mean

On a task, `running on <computer>` means its worker runs on that paired
computer while this chat stays here. Its steps appear here in order as it
works. `ran on <computer>` records where completed work ran;
`interrupted on <computer>` means the worker stopped without leaving an answer.
These words appear on task cards, the rail, Tasks rows and the task page.

The task's change returns to its copy here and follows normal landing. Private
task copies wait for Review and Apply here. Conflicting changes are kept for
your decision. If the task copy here changed while the other computer worked,
codeaf says `the task copy changed here while the other computer worked; both copies were kept`.

This differs from a whole chat held on another computer: task placement keeps
your conversation and transcript here, and only sends the task's own copy.
The remote worker uses that computer's provider keys; its spend is attributed
to this chat and task on this computer too.

## My other computer stopped answering

After about a minute without an answer, the task says
`<computer> stopped answering — it may still be working` and offers `run it here`.
The worker may still be running there. codeaf keeps trying; when the connection
returns, the card says `running on <computer>` again and missed steps arrive
in order. Reopening this chat follows the saved attempt and brings its result
home once. Nothing is ever re-run by itself to recover a remote attempt.

Restarting `codeaf serve --take-tasks` follows a child that is still alive. A child that died
without an answer is interrupted and is never started again automatically.
Stopping the task here asks the other computer to stop; its partial change
comes back through the same landing road as a stopped local task.

If the computer never took the task, the sentence is
`<computer> is not taking tasks right now — run codeaf serve --take-tasks on it`.
An older build says
`<computer> runs a different codeaf that cannot take this task — update both`.
A removed device is refused with `this computer was removed from your devices`.

## Run it here

When the other computer is unavailable or its task was interrupted, the task
card and task page offer `h run it here`. Click the card action or press `h`
over an empty message box. The action is absent while that computer is still
answering normally; stop the task first in that case.

Choosing `run it here` abandons the old attempt and starts fresh local work in
the unchanged copy here. A late result from the abandoned attempt never lands.
The placement words disappear and the receipt says
`running task <number> <title> here`. If this copy has changed, codeaf keeps
both copies and refuses to overwrite it. The abandoned task’s record is archived;
the local worker starts with a fresh record of effects in its own copy. A computer that dropped may still be
working; a request to stop it is best effort.

## Run tasks on — when the model gets the computer argument

In `/settings` → Tasks, **Run tasks on** follows **Task model**. Its hint is
`where a task goes when you do not name a computer`. It offers `this computer`
(the default), then eligible paired computers by shown name. The row reads the
computer list only when you open it. It is absent on unpaired, quiet or Windows
homes, including from the model’s `settings` and `change_setting` tools. A removed or unsupported saved choice displays
`this computer`. Changing the row affects the next task, not work already running.

An explicit `/task --on`, proposal-card computer choice, or model-supplied
`computer` wins over the setting. Omitted means the setting decides. Quick
tasks and other work without an independent copy run here even when the
setting names another computer, without a special placement sentence.

## codeaf serve --take-tasks takes tasks from paired computers

Plain `codeaf serve` opens only the machine connection and does not take tasks.
Run `codeaf serve --take-tasks` on the paired computer that should take tasks. It prints
`takes tasks from your paired computers`. Pairing with the same identity is
the permission; no separate machine-access grant is needed for tasks. Quiet or
unpaired homes do not offer this door. Linux and macOS are supported on both
ends; Windows is not yet. A named unsupported target says
`<computer> cannot take tasks from other computers yet`. Windows serving says
`this computer cannot take tasks from other computers yet`.

At most two attempts run on that computer at once. A third is refused with
`<computer> is already running two tasks`. If its provider key is missing, the
task ends incomplete with that computer's explanation. The other computer's
keys pay and its spending permission still applies. Without a time limit from
this chat, a remote attempt gets six hours. A time limit sent by this chat
replaces that wall; it cannot lift the other computer's spending permission.
Restarting serve follows
an existing child; it never repeats an admitted attempt's tools.

## Why did my task run here instead

Only a task that works in one independent copy can go to another computer.
Quick tasks, program runs, explicit in-place tasks, reference or scratch work,
folders without a transportable copy, and tasks spanning several roots stay
here. A singleton private task copy can go. Linux and macOS are supported on
both ends; Windows is not yet.

When only **Run tasks on** selected the computer, ineligible work runs here
without special placement text. When you named it explicitly, codeaf explains,
for example: `a quick task runs here — only a task that works in its own copy can run on <computer>`.
A computer that is not serving does not silently redirect work: it offers
`run it here` for your choice.

## Can this conversation start other tasks while one runs on another computer

A task on another computer runs alone in this conversation. You can keep
talking here, but wait for that task to finish before starting more tasks,
including another task on the same computer. The refusal is
`the work already underway uses another computer; let it finish before starting this task`.
This limit applies even though the receiving computer can take two attempts
from separate conversations. Remote tasks cannot join more work into the
instruction already sent.

## Could not check your devices right now

`could not check your devices right now` means the task computer could not
refresh your devices list. It refuses new work and ends the connection rather
than trust an old permission. Check sync connectivity on both computers, then
retry. Removing a device also ends an existing task watch when its cached
permission expires, within about a minute; the worker itself may still run.
`this computer was removed from your devices` requires pairing that computer
again before it can send tasks.

## Could not confirm this is my task computer

`could not confirm this is <computer>` or `could not confirm this computer`
means the connection could not prove it was the chosen paired computer.
Check its entry in Devices, update codeaf on both ends, and pair again if needed.
Do not substitute another computer just because its name looks the same.
`<computer> runs a different codeaf that cannot take this task — update both`
requires updating both ends before retrying.

## The task copy could not be sent

`the task copy changed while it was being sent; try again` keeps the copy here.
Finish or pause the edits, then retry the task.
`task copies need private storage and a relay blob store` means this chat's
task transfer is not set up; reopen a supported paired chat with sync available.
`a home folder or a folder above it cannot be sent as a task copy` requires
choosing a project folder below your home, with an independent task copy.
No task is silently redirected elsewhere; use `run it here` when offered.

## The task copy cell or head is not readable

`the task copy's cell is not readable` and
`the task copy's head is not readable` mean the saved transfer information
cannot identify a usable task copy. Keep the copy here and the other computer's
work, update both ends, then retry from the task's own controls.
`the sent task copy's fingerprint is missing` means codeaf cannot safely
compare the returned work with what it sent. Keep both copies and choose which
change to bring home; do not hand-edit the saved comparison.
`the task copy's repository link is not a file` or
`the task copy is missing its independent history` requires keeping the copies
and preparing a new independent task copy before retrying.

## The task destination already contains files

`the task's destination already contains files` refuses to replace existing
files while receiving a task copy. Keep that folder intact, inspect the old
task's result, and retry with a fresh task copy. Do not clear a folder while a
worker may still use it.
`the task copy changed here while the other computer worked; both copies were kept`
also covers an edit made just as a returned result was being installed. Your
edit stays here and the other result stays available; inspect both before
choosing what to bring home.

## Could not put the result in place; the earlier copy is kept

`could not put the result in place; the earlier copy is kept at <path>: <reason>`
means installation and its rollback could not finish. Preserve the named path:
it holds your earlier copy. Check free disk space and folder permissions, then
inspect both copies before moving files or retrying. Do not delete the named
folder as temporary clutter.
`The result could not be brought from <computer>; the copy here was kept`
means the remote task has no usable returned copy. Keep your local copy, restore
connectivity, and inspect the task on the other computer before choosing
`run it here`.

## That task attempt id is not readable, still stopping, or already finished

`that task attempt id is not readable` means the saved attempt cannot be
identified; reopen the chat with an updated build and keep both task copies.
`that task is still stopping` means an abandoned worker has not finished
stopping; wait rather than start the same attempt again.
`that task has already finished` refuses replay of an acknowledged attempt.
Completed attempts keep compact replay protection for the newest 1,000
acknowledgements, even after restarting the serving computer. Start a new task
if you want the work performed again; resending its old attempt cannot rerun it.
These receipts describe the saved attempt, not a reason to discard its work.

## That task request is not readable

`that task request is not readable` means the receiving computer cannot read
the request. Update both ends, reopen the chat, and retry through the normal
task controls.
`choose landed or abandoned for this task` concerns the saved acknowledgement
of a result. Keep both copies and reopen the chat so codeaf can retry that
acknowledgement; do not invent an acknowledgement or resend the old start.
`taskdoor: a frame exceeds the 1048576-byte limit` means a task message exceeded
the transfer limit. Keep the copies, update both builds, and use a smaller
instruction or bounded task output before starting a new attempt.

## The other computer no longer has this task's answer

`the other computer no longer has this task's answer` means the saved watch
found no retained attempt. Keep the copy here; inspect the other computer's
task files before choosing `run it here`.
`the other computer sent an unreadable task event` means an event cannot be
used safely. Update both ends and reopen the chat to follow its saved attempt;
do not start another worker while the first may still run.
Reopening follows the original attempt and never repeats its tools by itself.

## The task's remote record cannot be saved here

`the task's remote record cannot be saved here` or
`the task's remote record is missing` means the conversation cannot keep the
information needed to follow work elsewhere. Keep both task copies, check
local disk space and permissions, and reopen the saved chat before retrying.
`could not save the task result: <reason>` similarly requires preserving the
other computer's task folder and fixing the named storage problem.
`the returned task copy must be landed by this computer` means the result
must go through the owning chat's ordinary landing or Review and Apply here;
open that chat to bring the work home.

## The task was interrupted before it left an answer

`the task was interrupted on this computer before it left an answer`,
`the task ended without an answer`, or `the task ended without a result`
means no complete answer was left by the worker. Its partial work stayed on
the other computer. The sent copy here remains unchanged so `run it here`
can start fresh; inspect partial work on the other computer if you need it.
`The work could not be sent back: <reason>` keeps that work on the other
computer too. Fix the stated transfer problem and keep the copy there until
you have brought the change home. A normal stop still brings partial work
back through the usual landing road.

## Could not record the task's child

`could not record the task's child: <reason>` means the serving computer could
not safely remember the worker it started. It stops that worker rather than
lose track of it. Inspect the reason, update both builds, and retry with a new
task after checking the retained copy.
Recovery may instead say
`the recorded process does not name this task's work folder`,
`the recorded process does not name this task's private record`,
`the child's process start is not readable`,
`the recorded process is not this task's child`, or
`this task's child is no longer running`.
These refusals prevent attaching to an unrelated process. Keep the task copy;
an interrupted attempt can be replaced with `run it here` after inspecting it.

## The task's saved event is too large

`the task's saved event is too large` means a recorded step or charge cannot
fit the bounded task journal. Keep the other computer's task folder and inspect
its output; reduce oversized output before starting a new task.
Saved journal damage can say
`taskdoor: the saved task events are out of order`,
`taskdoor: saved events follow the task's finish`,
`taskdoor: only task events belong in a journal`,
`taskdoor: unknown task outcome`, or
`taskdoor: that task has already finished`.
Preserve the record, update codeaf, and inspect the retained work before
choosing a fresh local attempt. Do not repair the journal by deleting lines.

## This computer is already taking tasks

`this computer is already taking tasks` means a serving process already owns
the task folder. Use that process or close it before starting another one.
`` this machine is already connected to sync as <connection> — there is only one of it, so close the other `codeaf serve` ``
requires closing the duplicate serving process.
`<computer> is already running two tasks` requires waiting for a slot; each
remote task still runs alone in its owning conversation.
Setup errors such as `taskdoor: a door needs a relay, roster and executor`,
`taskdoor: this device does not belong to the identity`,
`taskdoor: an attempt store folder is required`, or
`taskdoor: attempt <id> is not a folder` require checking the paired profile
and its task storage before starting `codeaf serve --take-tasks` again.

## The task door says empty frame or a frame needs a type

`taskdoor: empty frame` or `taskdoor: a frame needs a type` means the task
computer sent an unreadable task message. The connection cannot safely start or
follow that request. Update codeaf on both computers, check the chosen computer
in Devices, and reopen the chat before retrying through its task controls.
Keep the task copies while checking whether the original attempt started.
A connection failure never grants permission to repeat an admitted attempt.

## Why a task appears before its copy is ready

A task sent to another computer appears here before its local copy is prepared.
Preparing and sending that copy happens in the background, so a large project
does not hold up the start receipt. You can stop the task while it prepares.

If preparation fails, the task shows the reason instead of silently starting
local work. If the other computer is not serving, it still says
`<computer> is not taking tasks right now — run codeaf serve --take-tasks on it`
and offers `run it here`. Ordinary tasks on this computer keep their existing
start behavior.

## Reopening a task while its copy is being prepared

If you close the chat during preparation, reopening keeps the chosen computer
and adopts any copy already prepared. Retained partial copies stay named on
the failed task for inspection; they are never silently replaced.

## Paired task computers are unavailable here

`paired task computers are unavailable here` means this chat cannot currently
use its saved remote executor, for example after pairing becomes unavailable
or the home is made quiet. The pending task keeps its chosen computer for
when support returns; reopening never chooses local execution by itself.
