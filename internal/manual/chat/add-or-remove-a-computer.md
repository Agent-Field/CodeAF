# Adding a computer to your devices, approving it, and removing it

## How do I add a second device — the Add another machine card, codeaf pair, a link to paste, and pairing a server with no browser

Your chats and your work can follow you to a second device. You add it in two steps. There is no account to make and no form to fill.

While you have only one device, home shows a card at the foot:

```
+ Add another machine
Add another machine - pick up your work anywhere, exactly where you left it.
alt+d how
```

Press `alt+d` to open the steps, and `alt+d` again to close them:

```
1. On the new machine, install and run: codeaf pair
2. It shows a link. Paste it here:
>
```

1. On the new device, install codeaf and run `codeaf pair`. It makes a link and waits.
2. On this device, paste that link into the card, or paste it anywhere on home, or type it as your message there and press enter. The approve screen opens (see "Approve a new device" below). Text that is not a pair link stays in the box as before.

The card goes away once you have two devices. It is not drawn when this chat cannot pair at all, for example a chat that runs over `--host` or `--at`.

What the new device prints while it waits:

```
Approve this device from one you already use. Open this link there:
  https://codeaf.agentfield.ai/p/k7m2q9xd#Qm9v…

Or, on a computer with codeaf, run:
  codeaf pair approve k7m2q9xd.Qm9v…

Check number: 4821 (the other device shows the same number)
Waiting for approval; good for 10 minutes. Press ctrl+c to cancel.
```

A device with no screen or no browser, such as a server, does the same: run `codeaf pair` there and approve from your laptop. Two things name the same request: the link `https://codeaf.agentfield.ai/p/<code>#<key>`, which opens in a browser and hands over to the installed app (the part after `#` never leaves the browser), and the bare token `<code>.<key>`, for a terminal where nobody can click. `codeaf pair approve` takes either one, and so does pasting into home. When it is approved it says how many workspaces it can now reach:

```
Paired - 3 workspaces available.
```

(`Paired - 1 workspace available.` when there is one.)

## The pair words — approve, --code and --via

```
usage: codeaf pair [--replace] | codeaf pair approve <link-or-code> | codeaf pair <code> [--replace] | codeaf pair --code
```

- `codeaf pair` with nothing after it is for the NEW device. It asks to join and shows a link.
- `codeaf pair approve <link-or-code>` is for a device that is already in. It shows who is asking and asks `y / n`. Pasting the link into the chat, or typing `/pair <link>`, does the same thing on a screen.
- `codeaf pair --code` shows a six-digit code that gives your chats to another device, the older way. It is told in the page *Pairing your chats with a second device*.
- `--via <address>` names the address of the service to go through when it is not the one this device already uses. Pass it to both `codeaf pair` and `codeaf pair approve`. The new device prints the full approve line with `--via` in it when one is needed.

A link is good for 10 minutes and one approval. If it ran out, run `codeaf pair` on the new device again.

## The four-digit check — how I know the right computer is asking

The new device shows a check number of four digits. The device that approves shows the same four digits. Look at both screens. Approve only when they match. If they do not match, deny: somebody else may have used the link.

On the approving screen the number reads `Check number 4821 - it must match the one on the new device.` In a terminal the question is `Does that device show the check number 4821?` and you answer `y` or `n`.

## Approve a new device — the approve screen, a and d, and deny

When you open a link on a device that is already in (paste it, or type `/pair <link>`), the screen says who is asking:

```
A new device wants to join your fleet.
  $ laptop · Linux
  asked just now · 9 min left
Check number 4821 - it must match the one on the new device.
a approve · d deny · esc later
```

- `a` lets it in. Press nothing else: **enter does nothing here**, because a device that is let in can read your chats.
- `d` turns it away. The screen says `laptop was turned away.`
- `esc` closes the screen without deciding. The request stays open until it runs out, and the same link opens it again.

The line under the name says what system the new device runs (Mac, Linux, Windows, iPhone or iPad, Android, or unknown system), when it asked, and how many minutes are left. A request that ran out says `run out`, and the screen says `that request has run out - ask the new device for a new link`.

When you approve, your screen says:

```
laptop joined your fleet - your chats are now everywhere.
```

Every other device that is on says it too, a moment later, from the same message. In a terminal approval the line is `laptop joined your devices.` and a no says `Request declined.` The terminal first says `"laptop" (linux) wants to join your devices.`

## What does the dot next to a computer mean — the devices row, online and offline

When you have two or more devices, home shows one row of them:

```
● This Mac  ● spark  ○ dumb (offline)
```

A filled green dot `●` is a device that is online now, and always your own. A hollow dim dot `○` with `(offline)` is one that is not online: its lid is closed or codeaf is not running there. The row changes by itself within a second or two when a device comes or goes. If home cannot tell who is online, the row is not drawn, because a row that guessed would be wrong.

Under the row, `alt+m bring work here` brings the newest chat of an online device to this one. With more than one choice it asks `Which device?` and the last choice is `leave it there`. If there is nothing to bring it says `no other device that is online has a chat to bring here`. The page *Continuing a chat on another device* tells what happens next.

## Which devices do I have — /devices, how to remove one, and I was told this machine was removed

Type `/devices` to list them:

```
› ● This Mac  Mac  this device
  ● spark  Linux
  ○ dumb  Linux  seen 3h ago
↑↓ choose · r revoke · esc close
```

A filled dot is online, a hollow one is not, and `seen 3h ago` says when an offline device was last on. With only your own device it says `no other device is in your fleet yet - /pair adds one`.

To remove a device, move to its row and press `r`. It is removed at once and the row says `revoked`:

```
dumb was revoked - it can no longer reach your chats.
```

The list is in a fixed order: this device first, then devices that are online, then the rest, each by name. Two devices with the same name show a short tail, such as `spark #a1b2`, so you can tell them apart. Your own computer has no `r`: it cannot remove itself, and if you press `r` there the list says `this device cannot remove itself - choose another row.` A computer that is already revoked does nothing when you press `r`. A removed computer can only come back as a new request that you approve. Removing a computer does not undo what it already holds: for a lost computer read the page *Locking out a lost computer for good*.

**I was told this device was removed.** Another of your devices removed this one. The chat says:

```
this computer was stopped by another of your computers, so this chat stays here only; run `codeaf pair` to bring it back
```

It appears the moment the other device removes this one, even if codeaf has been open for days, in the open chat and on the home screen alike, and once only. Your chats on this device are safe and stay on it. They no longer follow your other devices. To bring the device back, run `codeaf pair` here, then approve the link it shows from a device that is still in (the steps above). If you did not mean to remove it, that is the whole fix.

## The link did not work — a pairing link or token that ran out, was declined or was answered already

These are the sentences a link pairing says when it stops, in the words the screen uses. Nothing was changed on either device when you read one of them.

- `that is not a codeaf pairing link or code; it looks like https://codeaf.agentfield.ai/p/k7m2q9xd#... or k7m2q9xd.<key>`: what was pasted is neither shape. Paste the whole link or the whole `<code>.<key>` token. A link from any other host is not a codeaf link.
- `the link ran out before anyone approved it; run `codeaf pair` again for a new one`: a link is good for 10 minutes.
- `that link is not waiting any more; run `codeaf pair` on the new device for a fresh one`: the link is unknown, already used or gone. Those are one answer on purpose.
- `that request was already answered`: another of your devices approved or denied it first.
- `the request was declined on your other device; run `codeaf pair` to ask again`: the new device sees this when you deny.
- `the check number does not fit that device, so nothing was approved`: the number on the approve screen does not belong to the device that asked. Do not approve; ask again.
- `this computer is not paired with any device yet, so it cannot approve one; run `codeaf pair` first`: you ran `codeaf pair approve` on a computer that is not in a fleet yet.
- `paired, but your devices could not be read yet; open codeaf again in a moment`: the pairing worked; the device list needs a moment.

Too many requests from one network are refused too: at most 10 new links in an hour and 3 waiting at once from one network. See *Pairing your chats with a second computer* for the older six-digit code and its own limit `too many pairings from this network; wait 7 min`.
