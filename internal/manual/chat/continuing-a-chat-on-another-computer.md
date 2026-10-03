# Continuing a chat that one of your machines holds

This page is about continuing a chat on another computer: the question home asks, the offer when a device is off, and the line that says how long the move took.

## Why can't I type in this chat — one of my machines is running it, Continue here, and what leave it there does

A chat can be worked on by one device at a time. If you open a chat here that another of your devices is running right now, and you type a message and press enter, the message does not go to the chat. Home opens instead, with that chat's row under the cursor and the question up:

```
Continue this chat here?
running on spark; last durable turn 4s ago
1 continue here
2 leave it there
```

Your words stay in the box. Nothing was sent, and no branch nobody asked for was made. This card came up because you sent a message, not because you chose to continue, so `leave it there` is already chosen and leaning on enter changes nothing. Press `1`, then enter, to continue the chat on this device.

This only happens in a window that has already read your other devices. A window that has not, sends as usual. Slash commands such as `/help` always work.

If you continue it, the other device's window stops taking new turns and says `<device> continued this chat; this window now only shows it`. The page *Home* has the rest of that card.

## What does running on spark mean — a row on home and the verb it offers

Home lists chats from your other devices. A chat that a device is working on now says `running on <device>`. One that a device let go of, or that went off because its lid closed, says `<device> off`.

Press enter on either row to continue it here. The verb on the card depends on the row: **Move here** for a chat another device is running right now, **Continue here** for one it let go of. You already chose to continue, so the cursor starts on `continue here` and enter completes the move; `esc` leaves it there. The card says how fresh the copy is, so you know how many turns may still be on the other device.

## How do I continue on this machine — Continue where you left off, alt+c and alt+x

If you open codeaf and a chat on another device was running or went off in the last 24 hours, and that device is offline now (its lid is closed) or has let go of the chat, home offers it first of all:

```
Continue where you left off on spark?
alt+c continue · alt+x not now
```

- `alt+c` opens the same card as above, with the reason and the cursor on `continue here`, so enter finishes it. Nothing is moved by this line alone.
- `alt+x` dismisses it for that device until you open codeaf again.

The line follows the devices row: it stands while the device is offline or has let go of the chat, even if that became true after you opened codeaf, and goes away if the device comes back. A device that is online and still working on the chat is never offered.

To bring work from a device that is online, use the devices row: `alt+m bring work here`. The page *Adding a device to your devices, approving it, and removing it* shows the row and the dots.

## What happens when the chat arrives — the banner Moved from spark, and what was not brought

When the chat opens on this device it starts with one line that says how long the move took:

```
Moved from spark in 3.2s. Everything as you left it.
```

If a command was running on the other device, the line goes on:

```
Moved from spark in 3.2s. Everything as you left it. What was running there can start again here.
```

When the other device's name is not known it reads `Moved here in 3.2s. Everything as you left it.`

Right after, a card opens that says where the chat stands. It is the first thing on the screen of the chat. When something has to be set up here, the card asks whether to bring this device to the state the other one was in (the page *Home* has that question). When nothing has to be set up, the card only reports, with one answer that closes it:

```
Moved from spark. Here is where it stands.
not committed yet: main.go, util.go · last tests: passed
got it
```

The lines under the heading are:

Each line is left out when it has nothing to say.

- `not brought along:` folders the copy left behind, and any log or saved output that was too big or looked like a secret, with the reason.
- `was running there:` commands that were running on the other device. They are listed so you can start them again.
- `also needed:` what this device also needs.
- `not committed yet:` the files with changes that are not committed, up to three names and then `and N more`.
- `last tests:` `last tests: passed`, `last tests: failed`, or `last tests: failed 2` when the run said how many. A line with no test run behind it is not shown.

On the set-up card, `set up` runs only the commands the card lists, and `not now` leaves it and says `/setup does this later`.

**The project label.** If the folder the chat was left in is not on this device, the chat works in a copy of it that came along, and the label at the right of the keys row says so: `project: proj-a (copy here)`, with the folder's name. If this device does not know the folder, the label shows the chat's name instead.

**If nothing happens when you press Continue here**, the cause is one of these:

- `another device continued this chat first`: another device was faster. The row goes back to `running on <device>`. Nothing changed.
- `other machines unreachable`: this device cannot reach the service. Nothing can be moved until it can.
- `could not continue this chat here`: the move failed. The row stays where it was; try again.
- With no way to continue set up, an off row has no `continue here`: enter says why.

## Taking a chat back, and two computers racing for the same chat — who wins, and what the loser sees

A chat has one holder at a time. To take a chat back from the computer that took it, continue it there the same way: press enter on its row on that computer and choose `continue here`. Nothing has to be returned or switched off first. A computer with the chat open when another takes it says `<device> continued this chat; this window now only shows it`, stops taking new turns, and keeps what it had not yet sent as a branch (`<K> turns from <device>: merge / discard`). A tool call the model tries there answers with that same line. Taking the chat back leaves a `node_modules` or other install folder that computer already has alone.

If two computers press `continue here` for the same chat together, only one of them gets it. The slower one sees `another device continued this chat first`, nothing is changed on it, and its row goes back to `running on <device>`. No turn is lost on either side: whatever the earlier holder had not sent is kept for you as the branch above. Pressing `2` (`leave it there`) on the question, or `esc`, changes nothing and leaves the chat where it is.

When the holder is off (its lid is shut), the row reads `<device> off` and you can still continue the chat here: the card says `last durable turn <N>s ago; up to <K> turns may still be on <device>`. Turns that never left that computer are not in the copy you get; when it comes back online it sees the chat was continued elsewhere and keeps those turns as a branch for you to merge or discard.

## Can the chat on the other computer still open the job logs it mentions — logs, saved tool output, task journals and the task history after a move

Yes. A chat's messages name files that sit beside the chat and not in your project: the log of a background job (`log at …/logs/jobs/2.log`), the full output of a tool result that was cut short (`full: logs/stubs/9c2f.txt`), and the journals of its tasks. They move with the chat. The other computer puts the logs and saved output back in the chat's own folder, so the same names open there. The task journals (each task's transcript, trajectory and saved output) are part of the chat itself, so they arrive whole inside it at the paths the chat reads them from. The first message it receives after the move says where they are now (`… that earlier messages name under <old folder> were carried: they are under <new folder> here`). A path in an old message still shows the old folder; use the new one.

Three limits keep a move light, and they apply to job logs and saved tool output only. A single such file over 1 MiB stays behind, and so does whatever does not fit in 16 MiB together (the oldest go first). A task journal has no size limit and always moves whole. A file that looks like it holds a secret (a key, a token) stays behind too. Each one is named under `not brought along:` with the reason and `stayed on the machine that made it`; read it there by asking that machine, and never assume the other computer has it.

Two things are made again on the other computer and are not carried: the project's task history (the list of finished tasks home and the tasks tool show), which is rebuilt from the chat's own record, and the git bookkeeping inside a task's working copy. The program's own files inside a task's working copy (the `.codeaf` folder, where a task's job logs sit) are not carried either, because they are not your work: they never show up on the other computer as a change you did not make. A file you made in the working copy does move. A task that finished on a kept branch keeps that branch in your project on the other computer, at the same commit.
