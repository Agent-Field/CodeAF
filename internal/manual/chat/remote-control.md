# Remote control: working in a chat that keeps running on another of your computers

## See my chat from my other computer — open it live, drive my desktop from my laptop, remote control, work in a chat without moving it

Remote control lets this computer open a chat that another of your computers is running, and work in it without moving it. The chat opens **live**. On Home, press `enter` on a row that says `working on tower` or `held on tower`. The chat opens marked `on tower · live`, its box says `Message tower`, and what you see is what tower is doing as it happens. Typing, steering and stopping act on tower, in tower's folder, with tower's files and tools. Nothing moves, and tower's own window keeps working. `/status` adds a row that says how it travels, such as `live · relay`.

Three things have to be true. The two computers are paired. Both are running codeaf, because the other computer has to be there to answer. And remote control is on, on both; it is off until you turn it on (see *Remote control is off*). A chat that no computer is running right now cannot be opened live. Remote control never starts a chat on another computer. For a chat that is not running anywhere, `enter` does what it always did, and moving it here is the only way to carry on.

## Open it live or move it here — Move here versus remote control, which one does enter do

Live leaves the chat, its files and its tools on the computer that runs it, and you work from here. **Move here** brings the chat to this computer, so it runs here from then on.

With remote control on and the other computer online, `enter` on a chat that computer is running opens it live. The `Move this chat here?` card is behind `alt+m` on Home (bring work here), `alt+c` on the resume card and the **Move here** verb of the devices row. A computer that is offline still asks the card, because there is nothing to open live.

To move a live chat, press `alt+m` inside it. It asks `Move this chat here?`; answer `1`, and the view says `Moving here…` while the chat arrives, then the chat runs on this computer. The other computer's window of that chat becomes a live view of this one. If the move fails, the chat stays where it was, you are back in the live view, and nothing you typed is lost. The older names for moving a chat are answered on the page *Continuing a chat that one of your machines holds*.

## Watch only — what does watch only mean, the box says watch not type

Each of your computers can be allowed to **watch** a chat that another computer runs, or to watch and **type** into it. A computer that may only watch sees everything live, and its box is replaced by:

```
tower lets this computer watch, not type. Change that in /devices there.
```

Typing sends nothing and the words you typed stay. The switch is on the computer that runs the chat, not on the one that watches. On that computer open `/devices` (or `/settings` → `Devices`) and move to the other computer's row. It says what that computer may do: `watch and type`, `watch only` or `not shared`. The key line gains `w watch` and `t type`.

`w` turns watching on or off. Turning it off says `tower can no longer watch chats on this computer.`, closes its open views and turns typing off too, because a computer that cannot watch cannot type. Turning it on says `tower can watch chats on this computer.` `t` turns typing on or off. Turning it off says `tower can watch, not type.` Turning it on asks first:

```
let tower type into chats on this computer? y turns it on, any other key keeps it off
```

`y` turns it on and says `tower can type into chats on this computer.` Any other key, `esc` included, keeps it off. From a script, `codeaf devices permit "laptop" --drive on`.

## Home asks whether my other computers may type — older paired computers, let them type, keep them watching

A computer paired before remote control existed starts watch only, and a computer paired afterward starts with watch and type both on; the pairing screen says so. When you have computers paired earlier, Home asks once:

```
Let your other computers type into chats here?
tower can watch your chats here but cannot type yet.
1 let them type    2 keep them watching    3 choose in /devices
```

`1` lets them type and `2` keeps them watching. `3` keeps them watching and opens `/devices`, where each computer has its own `w` and `t`. Either answer ends the question for good. `esc` leaves it unanswered, and it comes back the next time Home opens. A computer paired earlier could already move any of your chats, so letting it watch adds no new reach; typing is the new power, which is why it starts off.

## Who has the keyboard — two computers on one chat, take the keyboard, typing from another computer

Only one computer types into a live chat at a time. The other one shows:

```
typing from tower now · enter types here
```

Press `enter` there to type from this computer instead. Words you typed while it was not your turn are kept, not sent, and are in the box when it is. This is the same hand-over two windows of one chat already do; scrolling, copy mode and Home still work while you are not holding the keyboard.

## The other computer went offline — last seen copy, my words are still in the box, last seen

When the computer running the chat stops answering, the view says `tower went offline`. It keeps what it last showed and the words in your box, and when tower comes back it picks up where it stopped, with no message missing and none shown twice. It says so within about 10 seconds while a turn is running or you have been using the keys, and within about 15 seconds on a view you are only looking at.

If tower is not running the chat when you open it, and this computer has seen the chat before, you get the copy this computer kept:

```
tower offline · last seen 14:02
```

It is read-only. If you press `enter` on words you typed it sends nothing: `not sent. tower is offline; your words are still in the box`. The copy lives only on this computer, as plain text in your codeaf folder, for the last 200 messages of up to 40 chats. Removing the computer, or moving the chat here, deletes it. Nothing in it was stored on the relay.

## Does remote control need relay storage — works with relay storage off, can the relay read what I type

No. Remote control works with relay storage off, while both computers are running codeaf. The relay passes encrypted bytes between them that it cannot read, and it keeps no copy of your chat text. What it can see while you work is how much encrypted traffic there is, when, and which of your computers are connected; the word *relay* here is the server, as everywhere else in codeaf.

What does need relay storage is a message for a computer that is **offline**: the desktop app can leave a waiting message for it, and the terminal cannot send anything to an offline computer at all. When the other computer is online, live typing needs no storage.

## Turn remote control off for a computer — allow watch only, remove a computer, stop my laptop controlling my desktop

Each computer has its own two switches, kept on the computer that runs the chats and nowhere else. On that computer open `/devices` (or `/settings` → `Devices`), move to the other computer's row and press `w` for watch or `t` for type; the row says `watch and type`, `watch only` or `not shared`. From a script, `codeaf devices permit "laptop" --watch off` or `--drive off`.

Turning **watch** off, or removing the computer, closes its open views at once. Turning **type** off leaves it watching. Removing a computer closes that device only: a computer you lost still holds your keys, so the remedy for a lost computer is `codeaf identity rotate`, as the page *Lock out a lost computer* says. Remote control is a power you give a paired computer. Turn type off for a computer you only want to look at your chats.

## What a remote viewer cannot do — answer a question that is waiting on my desktop from my laptop, irreversible questions, remembering a permission, raising approval

A computer that works in your chat live can do what you can do in a chat you are watching: type, steer, stop work and answer an ordinary question. It cannot answer a question the chat marks as irreversible, ask the computer to remember a permission, or raise how much the chat may do without asking. Those are the other computer's own to answer, and a viewer that tries gets:

```
That one needs your OK on tower itself. Answer it there.
```

Anything else that would change the other computer's settings or files is refused with `tower does not allow that from another computer.` The question stays open on every screen, and the computer running the chat decides.

## Disconnected after ten minutes idle — reconnect, daily limit, watch only until midnight

A live view that you have not touched for 10 minutes, with no turn running, lets go of the connection, and says:

```
Disconnected after 10 minutes idle. Enter to reconnect.
```

The view stays readable, and `enter` reconnects. Nothing is lost.

The computer running the chat also keeps a daily allowance of live traffic. When it is used up, every live view stays open and readable but turns watch only, and says `watch only until 00:00 UTC (daily limit)`. Typing works again after midnight UTC. The count survives a restart.

## Why did my live view close — not running this chat, does not share, closed this chat, too many computers

The view says why in these words:

- `tower is not running this chat right now.` Nobody is running it; open it from Home to move it.
- `tower does not share its chats with this computer. Change that in /devices there.` Watch is off for this computer.
- `tower closed this chat.` The chat ended there or was moved.
- `tower changed what this computer may do in its chats.` Reopen the chat to see the new setting.
- `this computer was removed from your devices` The computer must pair again.
- `Restart codeaf on tower to share this chat.` Tower runs an older codeaf; close it there and open it again.
- `tower is closing codeaf.`, `Catching up with tower.`, `One message was too large to show live.`
- `tower has too many computers open on its chats right now. Try again in a minute.`

## Open on desktop app — Open on a computer in WorkAF, Message the computer in the app

The desktop app can open a chat another computer is running too. On such a chat it offers **Open on tower**, the strip says it is live, and its box reads `Message tower`. Move here is its other button. This needs a desktop app version that includes remote control; an older one keeps the single Move here button, and nothing in the terminal depends on it.

## Remote control is off — how to turn it on, remote control says restart codeaf on the other computer

Remote control is off until you turn it on, on both computers: open `/settings` → `Devices` and turn `remote control` on, or start codeaf with `CODEAF_REMOTE_CONTROL=1`. With it off, everything works as before: `enter` on another computer's chat asks to move it, and no computer can open yours live.

Both computers need a codeaf that includes it. If the other one still runs an older codeaf in the background, the view says `Restart codeaf on tower to share this chat.`: close codeaf there and open it again.

## Remote control versus --at, --host and codeaf serve

`codeaf chat --at`, `--host` and `codeaf serve` are unchanged and need no pairing: they work on a machine you reach by name, and `--at` may start a chat there. Remote control is different. It is for your own paired computers, it opens only chats they are already running, and it starts none. The page *Reaching this machine without ssh* compares them.
