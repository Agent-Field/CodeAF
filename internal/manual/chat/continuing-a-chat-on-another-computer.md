# Continuing a chat that one of your machines holds

This page is about continuing a chat on another computer: the question home asks, the offer when a device is off, and the line that says how long the move took.

## Why can't I type in this chat — one of my machines is running it, and Continue here

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

- `not brought along:` folders the copy left behind.
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
