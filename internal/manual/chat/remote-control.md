# Remote control: working in a chat that keeps running on another of your computers

## See my chat from my other computer — open it live, drive my desktop from my laptop, remote control, work in a chat without moving it

Remote control lets this computer open a chat that another of your computers is running, and work in it without moving it. The chat opens **live**. On Home, press `enter` on a row that says `working on tower` or `held on tower`. The chat opens marked `on tower · live`, its box says `Message tower`, and what you see is what tower is doing as it happens. Typing, steering and stopping act on tower, in tower's folder, with tower's files and tools. Nothing moves, and tower's own window keeps working. `/status` adds a row that says how it travels, `live · direct` or `live · relay`.

Three things have to be true. The two computers are paired. Both are running codeaf, because the other computer has to be there to answer. And remote control is on, on both; it is off until you turn it on (see *Remote control is off*). A chat that no computer is running right now cannot be opened live. Remote control never starts a chat on another computer. For a chat that is not running anywhere, `enter` does what it always did, and moving it here is the only way to carry on.

## A new chat on my other computer's Home — when it appears, while its first message is still running, working on <name>, why it says untitled

With remote control on, a chat shows on your other computers' Home as soon as its first turn **starts**, marked `working on <name>`, and `enter` opens it live. Without remote control it appears when that first turn ends, as it always did. A chat nobody has typed in appears nowhere, so an opened and abandoned chat leaves no untitled row. Only the row travels this early: which computer runs the chat and that it is working, with no title, no folder and no words. To make that row the chat is saved once as it stands when the turn starts, which is your first message and no reply. With relay storage off that saved copy stays on the computer running the chat.

With relay storage off, which is the default, the row never carries the chat's title, so on another computer's terminal Home it reads `untitled` while the first turn runs and after it ends, and after every later turn. The server in between is never given the name of a chat, so the title is not sent. The terminal fills it in from a copy of the chat that computer already holds, and a chat that was started on another computer has none there. WorkAF asks the computer running the chat for the title directly (see *Why a chat shows as untitled on my other computer*). With that storage approved, or a self-hosted server run with `--store`, the row gets its title and its latest state when the turn ends, like any chat.

If the computer running it stops during that first turn, the row stays as `untitled` until the chat is opened again there. Opening it from another computer asks first. What it can bring is the chat as last saved, at most your first message and no reply, and only with that storage on: with it off, nothing comes while that computer is off.

## Open it live or move it here — Move here versus remote control, which one does enter do

Live leaves the chat, its files and its tools on the computer that runs it, and you work from here. **Move here** brings the chat to this computer, so it runs here from then on.

With remote control on and the other computer online, `enter` on a chat that computer is running opens it live. The `Move this chat here?` card is behind `alt+m` on Home (bring work here), `alt+c` on the resume card and the **Move here** verb of the devices row. A computer that is offline still asks the card, because there is nothing to open live.

To move a live chat, press `alt+m` inside it. It asks `Move this chat here?`; answer `1`, and the view says `Moving here…` while the chat arrives, then the chat runs on this computer. The other computer's window of that chat becomes a live view of this one. If the move fails, the chat stays where it was, you are back in the live view, and nothing you typed is lost. The older names for moving a chat are answered on the page *Continuing a chat that one of your machines holds*.

## Home's project after a live view — does opening a chat live change my project, the project shows a folder from my other computer

A live chat runs in a folder on the other computer, and that folder is not on this one. Inside the live view the chat names it as its own place, but Home does not take it as its project. The `project: <path>` label above Home's box, the first row of `projects` and the folder the next conversation opens in are the same while a live view is open as they were before you opened it, and the same after you close it. A project you picked with `alt+p`, `/project` or a click stays picked.

This is only for live views. Opening a chat that runs on this computer, in whatever folder it was started in, still makes that folder Home's project, as it always did.

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

## Direct or relay — does my remote view go through the relay, can my two computers connect directly, how do I see if remote control is direct, why does status say live direct

A live view starts through the server in between, so the chat appears at once. About half a second later the two computers try to connect straight to each other. If that works, the view moves to the direct road and `/status` has a `road` row that says `live · direct`. If it does not, the view stays where it was and says `live · relay`. Both are normal, and neither prints a banner. Two computers on one network connect directly.

Moving between the roads loses nothing. Words you typed are sent once, a turn that is running keeps going, and the keyboard stays with the computer that had it. A direct connection that stops working hands the view back to the server in between within 5 seconds when the server in between is reachable, again with no notice.

Traffic on the direct road does not count toward the daily allowance on *Disconnected after ten minutes idle*, and that allowance never stops a view that is already direct. A view you leave idle for 10 minutes lets go of both roads.

## Only use the relay — make remote control stay off the direct road, turn off direct connections, what is STUN and does codeaf contact Google, does remote control work on a network that blocks UDP

A computer behind a network that blocks UDP, and a phone on mobile data, usually cannot connect directly, so their views stay on the server in between, and remote control works the same there. Nothing needs setting.

While two of your computers connect directly, each learns the other's network address, and a public STUN server (Cloudflare's, then Google's) is asked for this computer's public address. Nothing about your chats goes to a STUN server, and what the server in between carries is still only encrypted bytes. Set `CODEAF_REMOTE_ROAD=relay` to keep every view on the relay and contact no STUN server; it works on either computer.

`CODEAF_REMOTE_STUN` replaces the two servers with your own, as a comma list of `host:port`, and `off` asks none, which still connects two computers on one network. Turning remote control off turns all of this off.

## Remote control and my organization — does it work for my organization's chats, hosted chats, shared chats, my team, several of my computers in an organization

Remote control is for the chats **your own computers** run. An organization's chats are not those. A chat in an organization's account runs on the organization's own server, and every window of it, on this computer, on another of your computers or in the desktop app, connects to that server and not to your other computers. So the switch under **Settings** and the `w` and `t` switches in `/devices` do not apply to them, turning remote control off does not close a window on one, and a chat in your personal account is never listed in an organization's account or the other way round.

What a person may do in an organization's chat is set per chat, by sharing it. Its owner may do everything. Someone it is shared with watches it live, and types, steers or answers only if their role holds `run`, `steer` or `answer`; the page *what can someone I shared my hosted chat with do* has the list. A person your chat is shared with is not one of your paired computers and cannot open your other chats.

Several of your computers can have the same organization chat open. Each is a window, one of them has the keyboard and the others read `typing from <name> now · enter types here`, the same rule as *Who has the keyboard*: the window that opened last takes it, and `enter` in another takes it back. In a chat shared with people the name is the person's, as the organization knows them. Which server carries the live views between your own computers is chosen the way pairing chooses it (`CODEAF_SYNC_URL`, or the address a pairing saved), and every computer you pair has to use the same one; joining an organization does not change that.

## Who has the keyboard — two computers on one chat, take the keyboard, typing from another computer

Only one computer types into a live chat at a time. The other one shows:

```
typing from tower now · enter types here
```

That line stands where the box would be, so words already in the box are kept but not drawn, and a view that is not holding the keyboard always says so. When the other computer has not told this one its name, the line reads `typing from the other computer now · enter types here`.

Press `enter` there to type from this computer instead. That first `enter` only takes the keyboard and sends nothing: the box comes back with any words it held when the other computer took the keyboard, and the next `enter` sends them once. Letters and commands typed while you are not holding the keyboard go nowhere, `/status` included, so press `enter` first and then type. This is the same hand-over two windows of one chat already do; scrolling, copy mode and Home (`space space`) still work while you are not holding the keyboard.

## The other computer went offline — last seen copy, my words are still in the box, last seen

When the computer running the chat stops answering, the view says `tower went offline`. It keeps what it last showed and the words in your box, and when tower comes back it picks up where it stopped, with no message missing and none shown twice. It says so within about 10 seconds while a turn is running or you have been using the keys, and within about 15 seconds on a view you are only looking at.

If tower is not running the chat when you open it, and this computer has seen the chat before, you get the copy this computer kept:

```
tower offline · last seen 14:02
```

It is read-only. If you press `enter` on words you typed it sends nothing: `not sent. tower is offline; your words are still in the box`. The copy lives only on this computer, as plain text in your codeaf folder, for the last 200 messages of up to 40 chats. Removing the computer, or moving the chat here, deletes it. Nothing in it was stored on the relay.

## Does remote control need relay storage — works with relay storage off, can the relay read what I type

No. Remote control works with relay storage off, while both computers are running codeaf. The server in between passes encrypted bytes between them that it cannot read, or, when the two can reach each other, they connect directly and none of it passes through the server in between. It keeps no copy of your chat text. What it can see while you work is how much encrypted traffic there is, when, and which of your computers are connected; the word *relay* on this page always means that server, as everywhere else in codeaf.

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

## Why did my live view end — nobody is running it, watch is off, it ended there, remote control was turned off, too many computers

The view says why in these words:

- `tower is not running this chat right now.` Nobody is running it; open it from Home to move it.
- `tower does not share its chats with this computer. Change that in /devices there.` Watch is off for this computer.
- `tower closed this chat.` The chat ended there or was moved.
- `tower changed what this computer may do in its chats.` Reopen the chat to see the new setting.
- `tower turned remote control off.` Remote control was switched off on tower, which ends every live view of its chats. The view stays ended and does not redial. When it is back on, open the chat from Home again.
- `this computer was removed from your devices` The computer must pair again.
- `Restart codeaf on tower to share this chat.` Tower runs an older codeaf; close it there and open it again.
- `tower is closing codeaf.`, `Catching up with tower.`, `One message was too large to show live.`
- `tower has too many computers open on its chats right now. Try again in a minute.`

## Open on desktop app — Open on a computer in WorkAF, Message the computer in the app

The desktop app can open a chat another computer is running too. On such a chat it offers **Open on tower**, the strip says it is live, and its box reads `Message tower`. Move here is its other button. This needs a desktop app version that includes remote control; an older one keeps the single Move here button, and nothing in the terminal depends on it.

## Remote control is off — how to turn it on, do I need to restart codeaf after turning remote control on, remote control says restart codeaf on the other computer

Remote control is off until you turn it on, on both computers: open `/settings` → `Devices` and turn `remote control` on, or start codeaf with `CODEAF_REMOTE_CONTROL=1`. In the desktop app it is the **Remote control** switch in Settings → Devices (*Turn remote control on or off in the desktop app*). With it off, everything works as before: `enter` on another computer's chat asks to move it, and no computer can open yours live.

You do not need to restart codeaf in the terminal. Turning it on in a window that is already open takes effect at once, both ways: the next `enter` on a chat another computer is running opens it live, and another computer can open yours. Turning it off does the same at once: `enter` asks to move the chat again and any computer that tries to open a chat of yours is refused, and the live views other computers already have open end within about half a minute. The chats here keep running and keep everything that was typed.

On the computer that was watching, the view ends with `tower turned remote control off.` and nothing else: it does not say `tower went offline`, it does not start `reconnecting to tower`, and it does not try again by itself, because tower is running and said no. When remote control is back on there, open the chat from Home again.

Both computers need a codeaf that includes it. If the other one still runs an older codeaf in the background, the view says `Restart codeaf on tower to share this chat.`: close codeaf there and open it again.

## Remote control versus --at, --host and codeaf serve

`codeaf chat --at`, `--host` and `codeaf serve` are unchanged and need no pairing: they work on a machine you reach by name, and `--at` may start a chat there. Remote control is different. It is for your own paired computers, it opens only chats they are already running, and it starts none. The page *Reaching this machine without ssh* compares them.
