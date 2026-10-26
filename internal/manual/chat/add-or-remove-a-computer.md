# Adding a computer to your devices, approving it, and removing it

## How do I add a second device — the Add another device card (add another machine), codeaf pair, a link to paste, and pairing a server with no browser

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

Your chats and your work can follow you to a second device. You add it in two steps. There is no account to make and no form to fill.

While you have only one device, home shows a card at the foot:

```
+ Add another device
Add another device — pick up your work anywhere, exactly where you left it.
alt+d how
```

Press `alt+d` to open the steps, and `alt+d` again to close them:

```
1. On the new device, install and run: codeaf pair
2. It shows a link. Paste it here:
>
```

1. On the new device, install codeaf and run `codeaf pair`. It makes a link and waits.
2. On this device, paste that link into the card, or paste it anywhere on home, or type it as your message (on home, on the start page of a new chat, or in any chat) and press enter. The link is never sent to the model. The approve screen opens (see "Approve a new device" below). Text that is not a pair link stays in the box as before.

The card goes away once you have two devices. It is not drawn when this chat cannot pair at all, for example a chat that runs over `--host` or `--at`.

## Pairing a server with no browser — the link, token and check number

On the new computer, `codeaf pair` prints this while it waits:

```
Approve this device from one you already use.

Open this link there:
  https://codeaf.agentfield.ai/p/k7m2q9xd#Qm9v…

Or, on a computer with codeaf, run:
  codeaf pair approve k7m2q9xd.Qm9v…

Check number: 4821 — the other device shows the same number.
Waiting for approval (10 minutes). Press ctrl+c to cancel.
```

A device with no screen or no browser, such as a server, does the same: run `codeaf pair` there and approve from your laptop. Two things name the same request: the link `https://codeaf.agentfield.ai/p/<code>#<key>`, which opens in a browser and hands over to the installed app (the part after `#` never leaves the browser), and the bare token `<code>.<key>`, for a terminal where nobody can click. `codeaf pair approve` takes either one, and so does pasting into home. When it is approved it says how many workspaces it can now reach:

```
Paired. 3 workspaces available.
```

(`Paired. 1 workspace available.` when there is one.)

## What does codeaf pair --code do

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

`codeaf pair --code` shows a six-digit code on this computer. On the other computer, run `codeaf pair <code>` to share this computer's chats. Compare the three words and approve on the computer showing the code. The page *Pairing your chats with a second computer* explains each step.

## The pair words — approve, --code and --via

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

```
usage: codeaf pair [--name <name>] | codeaf pair approve <link-or-code> | codeaf pair <code> [--replace] [--name <name>] | codeaf pair --code
```

- `codeaf pair` with nothing after it is for the NEW device. It asks to join and shows a link.
- `codeaf pair approve <link-or-code>` is for a device that is already in. It shows who is asking and asks `a / d`. Only `a` approves; `d`, Esc, Enter and anything else decline. Pasting the link into the chat, or typing `/pair <link>`, does the same thing on a screen.
- `--name <name>` is what your devices call this computer. See the page *Naming a device*.
- `--via <address>` names the address of the service to go through when it is not the one this device already uses. Pass it to both `codeaf pair` and `codeaf pair approve`. The new device prints the full approve line with `--via` in it when one is needed.

With a self-hosted address (`CODEAF_SYNC_URL=<url>` or `--via <address>`), the link opens that service's own `/p/<code>` page, and the terminal approval token includes `--via <address>`. It never prints a hosted page link for that invitation. The page shows the token command and an app link that keep the same address; the key stays after `#`. An address without a web page uses only the terminal token. Pasting a self-hosted link into a chat configured for another address sends no request and says `this pairing link uses another sync address — approve it in a terminal with the command shown on the new device`.

A link is good for 10 minutes and one approval. If it ran out, run `codeaf pair` on the new device again. Ctrl+c while it waits ends with `Cancelled. Nothing was paired.`

## The four-digit check — how I know the right computer is asking

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

The new device shows a check number of four digits. The device that approves shows the same four digits. Look at both screens. Approve only when they match. If they do not match, decline: somebody else may have used the link.

On the approving screen the number reads `Check number 4821. Approve only if the new device shows the same number.` In a terminal the question reads the same: `a / d`. Only `a` approves; `d`, Esc, Enter and anything else decline. Code pairing keeps its `y / n` question.

## Approve a new device — the approve screen, a and d, and decline

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

When you open a link on a device that is already in (paste it, or type `/pair <link>`), the screen says who is asking:

```
A new device wants to pair.
  $ laptop · Linux
  asked just now · 9 min left
Check number 4821. Approve only if the new device shows the same number.
a approve · d decline · esc later
```

- `a` lets it in. Press nothing else: **enter does nothing here**, because a device that is let in can read your chats.
- `d` declines it. The screen says `laptop was declined.`
- `esc` closes the screen without deciding. The request stays open until it runs out, and the same link opens it again.

The line under the name says what system the new device runs (Mac, Linux, Windows, iPhone or iPad, Android, or unknown system), when it asked, and how many minutes are left. A request that ran out says `run out`, and the screen says `that request has run out — ask the new device for a new link`.

While your answer is on its way the screen says `approving…` or `declining…` and offers no keys but `esc close`. If your other devices do not answer in time it says `no answer from your devices — try again`, and `a` and `d` work again. When the request has run out, `a` and `d` are not offered at all.

When you approve, your screen says:

```
laptop is now paired. Your chats can continue there.
```

Every other device that is on says it too, a moment later, from the same message. In a terminal approval the line is `laptop is now paired. Your chats can continue there.` and a no says `Declined. Nothing was paired.` The terminal first says `laptop (Linux) wants to pair.` (the system is left out when it is not known). `codeaf pair approve` with nothing after it says `usage: codeaf pair approve <link-or-code> — the link or code the new device shows`.

## What does the dot next to a computer mean — the devices row, online and offline

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

When you have two or more devices, home shows one row of them:

```
● This Mac  ● tower  ○ desk (offline)
```

A filled green dot `●` is a device that is online now, and always your own. A hollow dim dot `○` with `(offline)` is one that is not online: its lid is closed or codeaf is not running there. A computer counts as online only while it keeps answering: when another computer has not answered for 25 seconds (it is asleep, frozen, switched off or disconnected) its dot goes hollow and reads `(offline)`, within 30 seconds of its last answer, and a frozen computer no longer shows as online for minutes. It is back at once when it answers again. The row changes by itself, with no key to press, when a device comes or goes. As a backstop, home also reads the list again every 5 minutes while it is connected, and every minute while it is not. If home cannot tell who is online, the row is not drawn, because a row that guessed would be wrong.

Under the row, `alt+m bring work here` brings the newest chat of an online device to this one. With more than one choice it asks `Which device?` and the last choice is `leave it there`. If there is nothing to bring it says `no other device that is online has a chat to bring here`. The page *Continuing a chat on another device* tells what happens next.

## Does the other computer appear on the device list straight after pairing — a just-paired device, devices empty after pairing

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Yes, on both sides. When a pairing finishes — a six-digit code on both screens, a link
typed into `codeaf pair approve`, either end of it — the computer it finished on writes
its own device record then and there, the same record every chat start writes. For a link approved from the chat card or `codeaf pair approve`, the approving computer writes its record before the new device can receive `Paired.`; if that write fails, approval can be tried again and the request has not been granted. `codeaf
devices` and `/devices` show both computers at once, on each computer, with no chat
opened first. With the six-digit-code route, if that record did not land, the pairing still stands and the terminal
says one line about it, `paired, but this device could not write its own record: … — it
is written at the next chat start.`, and the pairing ends cleanly.

## Which devices do I have — /devices, how to remove or revoke one, a device that was revoked, and I was told this machine was removed

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Type `/devices` to list them:

```
› ● This Mac  Mac  this device
  ● tower  Linux
  ○ desk  Linux  seen 3h ago
↑↓ choose · n rename · r remove · esc close
```

A filled dot is online, a hollow one is not, and an offline device says when it was last on (`seen 3h ago`), or just `offline` when it was never seen. With only your own device it says `no other device yet — /pair adds one`, and the keys shrink to the ones that work: `n rename · esc close`. `r remove` shows only on a row that can be removed, and `↑↓ choose` only when there is another row.

To remove a device (the command line calls this revoking: `codeaf devices revoke <name>`), move to its row and press `r`. It is removed at once and the row says `removed`:

```
desk was removed — it can no longer reach your chats.
```

The list is in a fixed order: this device first, then devices that are online, then the rest, each by name. Two devices with the same name show a short tail, such as `tower #a1b2`, so you can tell them apart. Your own computer has no `r`: it cannot remove itself, and if you press `r` there the list says `this device cannot remove itself — choose another row.` A computer that is already removed does nothing when you press `r`. A removed computer can only come back as a new request that you approve. If you remove a computer from another of your devices while this list is open, its row changes to `removed` here too, without closing the list. Removing a computer does not undo what it already holds: for a lost computer read the page *Locking out a lost computer for good*.

## Devices in the desktop — removal warnings, refresh and re-pairing

With an engine that supports device management, the desktop can list this
computer first and the other computers paired to the same identity. The engine
supplies its removal explanation for each active computer that this one may
remove, so it can be shown before confirmation. This computer and already removed
computers have no removal action. The result keeps the command line's full
explanation, including the warning about chats and keys already held there.

After a removal succeeds, an older pending roster response cannot restore the
computer's active state. If a refresh fails, the completed removal is kept; only
a roster at a newer directory version can change that state.

After pairing again from a terminal, refresh Devices. The running app engine
reads the new identity and device files and drops the previous pairing's list,
presence and removal refusal. Restarting the engine is not required. Switching
the sync address also drops facts learned from the previous service.

## I was told this device was removed — how to bring it back

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

If this computer was removed while its window was closed, the explanation appears on home on the next launch too. The launch check and the live check share one notice, so learning it both ways does not repeat it.

Another of your devices removed this one. The chat says:

```
this device was removed by another of your devices, so this chat stays here only — run `codeaf pair` to bring it back
```

`/devices` on the removed computer says the same line in place of the list. The terminal command `codeaf devices` gives its removal explanation once, without following it with `nothing is paired yet`. It appears the moment the other device removes this one, even if codeaf has been open for days, in the open chat and on the home screen alike, and once only. Your chats on this device are safe and stay on it. They no longer follow your other devices. To bring the device back, run `codeaf pair` here, then approve the link it shows from a device that is still in (the steps above). If you did not mean to remove it, that is the whole fix.

## The link did not work — a pairing link or token that ran out, was declined or was answered already

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

These are the sentences a link pairing says when it stops, in the words the screen uses. Nothing was changed on either device when you read one of them.

- `that is not a codeaf pairing link or code; it looks like https://codeaf.agentfield.ai/p/k7m2q9xd#... or k7m2q9xd.<key>`: what was pasted is neither shape. Paste the whole link or the whole `<code>.<key>` token. A link from any other host is not a codeaf link.
- `the link ran out before anyone approved it; run `codeaf pair` again for a new one`: a link is good for 10 minutes.
- `that link is not waiting any more; run `codeaf pair` on the new device for a fresh one`: the link is unknown, already used or gone. Those are one answer on purpose.
- `that request was already answered`: another of your devices approved or denied it first.
- `the request was declined on your other device; run `codeaf pair` to ask again`: the new device sees this when you decline.
- `the check number does not fit that device, so nothing was approved`: the number on the approve screen does not belong to the device that asked. Do not approve; ask again.
- `this device has no codeaf identity yet; start codeaf here once before approving another device`: this computer has never started codeaf, so there is no local identity to share yet. Start codeaf here once, then approve the other computer.
- `paired, but your devices could not be read yet; open codeaf again in a moment`: the pairing worked; the device list needs a moment.

Too many requests from one network are refused too: at most 10 new links in an hour and 3 waiting at once from one network. See *Pairing your chats with a second computer* for the older six-digit code and its own limit `too many pairings from this network; wait 7 min`.

## I quit on my laptop and opened my desktop after pairing

After a clean quit and final durable save, bare `codeaf` on another paired computer within 24 hours opens the released chat automatically. A sleeping or disconnected laptop still needs `alt+c` on Home. Local unsaved edits, another open local window, a named chat, `--host`, `--at` or `--session` prevent automatic continuation. A running chat never moves by itself. The arrival retains its `Moved from` banner and `not brought along:` facts; `esc` goes Home.

Personal memory and stored model, display, approval and limit defaults follow the paired computers too. Limits are shared as values; spending counted against them stays per computer. Automations and machine-specific paths and settings stay local.
