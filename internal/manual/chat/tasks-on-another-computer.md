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
computer. A misspelt name is refused with
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
both copies and refuses to overwrite it. A computer that dropped may still be
working; a request to stop it is best effort.

## Run tasks on — when the model gets the computer argument

In `/settings` → Tasks, **Run tasks on** follows **Task model**. Its hint is
`where a task goes when you do not name a computer`. It offers `this computer`
(the default), then eligible paired computers by shown name. The row reads the
computer list only when you open it. It is absent on unpaired, quiet or Windows
homes. A removed or unsupported saved choice displays
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
ends; Windows is not yet. Windows serving says
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
