# Adding a computer to your devices, approving it, and removing it

## How do I add another computer — the Add another device card (add another machine), codeaf pair, a link to paste, and pairing a server with no browser

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. The original files stay untouched and remain usable in older codeaf builds.

Your chats and your work can follow you to a second device. You add it in two steps. There is no account to make and no form to fill.

While you have only one device, home shows a card at the foot:

```
+ Add another device
Pick up your work anywhere, exactly where you left it.
alt+d how
```

Click `+ Add another device` or press `alt+d` to open the steps. Click the heading or press `alt+d` again to close them. The heading underlines while the pointer is over it; the description and empty space do not act as buttons. The command `codeaf pair` stands out from the instructions, and the link field is shaded while it holds the keyboard:

```
1. On the new device, install and run: codeaf pair
2. It shows a link. Paste it here:
> Paste pairing link
```

## Paste the new device’s pairing link on Home

1. On the new device, install codeaf and run `codeaf pair`. It makes a link and waits.
2. On this device, click the card’s link field, type or paste the link, and press Enter. Opening the card also focuses its field. You can edit the link before submitting it; clicking inside the field places the caret there. Esc or Tab returns typing to the normal message box, and clicking the message box does the same. Your chat draft stays separate. You can also paste it anywhere on home when the card’s field is not focused, or type it as your message and press Enter. The approval replaces the steps on Home; approving, declining or closing it keeps you on Home. The link is never sent to the model (see "Approve a new device" below).

## Add a device through your own sync service — pairing instructions include --via

When this computer uses your own sync service, step 1 includes its address: `1. On the new device, install and run: codeaf pair --via http://127.0.0.1:8901`. `/pair`, Settings → Devices → Add another device, and `codeaf pair --code` include the same `--via` argument.

The card goes away once you have two devices. After that, `/pair` or Settings → Devices → Add another device still offers a place to paste the new device’s link, alongside the code command. It is not drawn when this chat cannot pair at all, for example a chat that runs over `--host` or `--at`.

## Do older chats move after pairing?

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

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

A device with no screen or no browser, such as a server, does the same: run `codeaf pair` there and approve from your laptop. Two things name the same request: the link (`https://codeaf.agentfield.ai/p/<code>#<key>` on stable releases and release candidates, `https://caf-fabric-staging.instrument-santosh.workers.dev/p/<code>#<key>` on staging builds, or `https://caf-fabric-dev.instrument-santosh.workers.dev/p/<code>#<key>` on dev and local source builds), which opens in a browser and hands over to the installed app (the part after `#` never leaves the browser), and the bare token `<code>.<key>`, for a terminal where nobody can click. Channels do not sync with each other: pair computers on the same release channel (or with the same `CODEAF_SYNC_URL`). `codeaf pair approve` takes either form, and so does pasting into home. When it is approved, it names one of the computers you joined and tells you how to open your chats:

```
Paired with laptop-a. Run codeaf to open your chats.
```

The approving device says `desk-b is now paired. Your chats can continue there.`

## What does codeaf pair --code do

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

`codeaf pair --code` shows a code such as `04-063-213` on this computer. On the other computer, run `codeaf pair <code>` to share this computer's chats. Compare the three words and approve on the computer showing the code. The page *Pairing your chats with a second computer* explains each step.

## The pair words — approve, --code and --via

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

```
usage: codeaf pair [--name <name>] | codeaf pair approve <link-or-code> | codeaf pair <code> [--replace] [--name <name>] | codeaf pair --code
```

- `codeaf pair` with nothing after it is for the NEW device. It asks to join and shows a link.
- `codeaf pair approve <link-or-code>` is for a device that is already in. It shows who is asking and asks `a / d`. Only `a` approves; `d`, Esc, Enter and anything else decline. Pasting the link into the chat, or typing `/pair <link>`, does the same thing on a screen.
- `--name <name>` is what your devices call this computer. See the page *Naming a device*.
- `--via <address>` names the address of the sync service to go through when it is not the one this device already uses. Pass it to both `codeaf pair` and `codeaf pair approve`. The new device prints the full approve line with `--via` in it when one is needed.

With a self-hosted address (`CODEAF_SYNC_URL=<url>` or `--via <address>`), the link opens that sync service’s own `/p/<code>` page, and the terminal approval token includes `--via <address>`. It never prints a hosted page link for that invitation. The page shows the token command and an app link that keep the same address; the key stays after `#`. An address without a web page uses only the terminal token. Pasting a self-hosted link into a chat configured for another address sends no request and says `that link is waiting on codeaf.agentfield.ai, but this computer syncs through 127.0.0.1:8901 — on the new device run: codeaf pair --via http://127.0.0.1:8901`.

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
  △ laptop · Linux
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

The plain repertoire uses a filled green dot `●` for online and a hollow dim dot `○` for offline. ASCII uses `#` and `o`; a patched font uses the same presence slots as `/devices`. Your own computer is filled while the strip is shown. A hollow dim dot `○` with `(offline)` is one that is not online: its lid is closed or codeaf is not running there. A computer counts as online only while it keeps answering: when another computer has not answered for 25 seconds (it is asleep, frozen, switched off or disconnected) its dot goes hollow and reads `(offline)`, within 30 seconds of its last answer, and a frozen computer no longer shows as online for minutes. It is back at once when it answers again. The row changes by itself, with no key to press, when a device comes or goes. As a backstop, home also reads the list again every 5 minutes while it is connected, and every minute while it is not. If the sync service has not answered for 25 seconds, Home cannot tell who is online and hides the strip. A stalled connection is subscribed again, so a late answer cannot keep an old online set alive. The strip returns with fresh presence; it never guesses that a silent service means every computer is offline.

Under the row, `alt+m bring work here` brings the newest chat of an online device to this one. With more than one choice it asks `Which device?` and the last choice is `leave it there`. If there is nothing to bring it says `no other device that is online has a chat to bring here`. The page *Continuing a chat on another device* tells what happens next.

## Does the other computer appear on the device list straight after pairing — a just-paired device, devices empty after pairing

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Yes, on both sides. When a pairing finishes — a code such as `04-063-213` on both screens, a link
typed into `codeaf pair approve`, either end of it — the computer it finished on writes
its own device record then and there, the same record every chat start writes. For a link approved from the chat card or `codeaf pair approve`, the approving computer writes its record before the new device can receive `Paired.`; if that write fails, approval can be tried again and the request has not been granted. `codeaf
devices` and `/devices` show both computers at once, on each computer, with no chat
opened first. With the code route, if that record did not land, the pairing still stands and the terminal
says one line about it, `paired; this computer will appear on your other devices after the next chat starts`, and the pairing ends cleanly.

## Which devices do I have — /devices, how to remove or revoke one, a device that was revoked, and I was told this machine was removed

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Type `/devices` to list them:

```
sync service: 127.0.0.1:8901 (your own)
  ● This Mac  Mac  this device
› ● tower  Linux
  ○ desk  Linux  seen 3h ago
  Add another device
↑↓ choose · n rename · r remove · esc close
```

A filled dot is online, a hollow one is not, and an offline device says when it was last on (`seen 3h ago`), or just `offline` when it was never seen. This computer is listed even before pairing, so `n rename` works on its row. `Add another device` follows the computers; select it and press `enter` to open `/pair`. On that row the keys are `↑↓ choose · enter add · esc close`. `r remove` shows only on a row that can be removed, and `↑↓ choose` only when there is another row.

## Remove a computer — r asks once, y removes, any other key keeps it

The removable row’s key line says `r remove`. To remove a device (the command line calls this revoking: `codeaf devices revoke <name>`), move to its row and press `r`. The card asks `remove desk? it must pair again to come back  ·  y removes  ·  any other key keeps it`. It uses the shown name, including a duplicate name’s `#tail`. Only `y` removes it; every other key, including `esc`, cancels and keeps that row selected without doing anything else. After `y`, the row says `removed`:

```
desk was removed — it can no longer reach your chats.
```

## Removed device rows, list order and a missing computer

The list is in a fixed order: this device first, then devices that are online, then the rest, each by name. Two devices with the same name show a short tail, such as `tower #a1b2`, so you can tell them apart. Your own computer has no `r`: it cannot remove itself, and if you press `r` there the list says `this device cannot remove itself — choose another row.` A computer that is already removed does nothing when you press `r`. A removed computer can only come back as a new request that you approve. If you remove a computer from another of your devices while this list is open, its row changes to `removed` here too, without closing the list. Removing a computer does not undo what it already holds: for a lost computer read the page *Locking out a lost computer for good*.

## Devices in /settings — where your computers are on the settings page, add another device, rename this one, or remove (revoke) one from settings

Open `/settings` (or `ctrl+,`) and walk with `←` and `→` to `Devices`, between `Connections` and `Privacy` — or type `devices`, `remove`, `revoke`, `pair` or `add device` in settings search.

While loading, the tab says `looking up your devices…`. Loaded, it begins with the same dim `sync service: 127.0.0.1:8901 (your own)` or `sync service: codeaf.agentfield.ai (hosted)` line as `/devices` and the terminal. It lists this computer even before pairing, with the same platform and presence columns and in the same order and words as `/devices` — `●` online, `○` away (`seen 3h ago`, or `offline`), `this device`, and `removed` — followed by `Add another device`. The open tab refreshes on its own when a computer joins, renames itself or is removed.

## Keys in Settings Devices — add, rename and confirm removal

In `/settings` → Devices, the key line shrinks to what works on the row (`↑↓ choose` when there is more than one row, `r remove` only on a removable computer, `←→ categories`, `esc close`):

- `enter add` on `Add another device` leaves `/settings` and starts `/pair`.
- `n rename` on `this device` opens `name this device: ` under the row with `enter save · ctrl+u clear · esc cancel`. While typing, `esc` cancels the edit without closing `/settings`; a second `esc` leaves `/settings`. On another row `n` says `a device renames itself — choose this device's row, or open /devices on the one you mean.`
- `r remove` (called `codeaf devices revoke <name>` on the CLI) asks `remove desk? it must pair again to come back  ·  y removes  ·  any other key keeps it`. Only `y` removes it, marks its row `removed`, and says `desk was removed — it can no longer reach your chats.` Any other key cancels, keeps the same row selected, and does nothing else. On your own row `r` says `this device cannot remove itself — choose another row.` On `Add another device` or a `removed` row, `r` does nothing.

Where devices cannot work, the tab shows one sentence and no rows: `pairing is not available here` (a chat over `--host` or `--at`), `moving chats needs a newer furrow engine than this build carries`, or the read error. With `CODEAF_SYNC_URL=off` it lists this computer, and `enter add` answers `sync is off (CODEAF_SYNC_URL=off); pairing needs the sync service`, as `/pair` does.

## A device was removed but refreshing the list failed

After a successful removal, its confirmation stays visible even if refreshing the device list fails. The card shows the refresh error too, then clears that error when a later refresh succeeds. A failed refresh does not restore the removed computer's access. If the removal itself fails, the card shows that error and does not confirm a removal.

## I was told this device was removed — how to bring it back

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Home shows `removed by another of your devices — pair again to sync` on its own dim line with the Add another device card. The line remains visible when this connection cannot offer that card. The footer keeps only key hints. `/pair` repeats that notice, then says `on a device still in your devices, open /pair; enter its code here to pair again` and shows `Code from another device`. Enter the other device’s code and confirm pairing. It cannot offer a code from the removed membership or approve another device’s link. If this computer was removed while its window was closed, the explanation appears on Home on the next launch too. The launch check and the live check share one notice, so learning it both ways does not repeat it.

Another of your devices removed this one. The chat says:

```
this device was removed by another of your devices, so this chat stays here only — /pair brings it back
```

## Can a removed computer rejoin — /devices, pairing again and retained local chats

`/devices` on a removed computer says the same line in place of the list. A terminal refusal uses `this device was removed by another of your devices, so this chat stays here only — run codeaf pair to bring it back`. Home says `removed by another of your devices — pair again to sync`. The terminal command `codeaf devices` gives its removal explanation once, without following it with `nothing is paired yet`. It appears the moment the other device removes this one, even if codeaf has been open for days, in the open chat and on the home screen alike, and once only. Your chats on this device are safe and stay on it. They no longer follow your other devices. To bring the device back, run `codeaf pair` here, then approve the link it shows from a device that is still in (the steps above). If this computer has chats with submitted turns, run `codeaf pair --replace` instead. Its existing chats stay on this computer but no longer sync; shared memories and settings are cleared. Approve the new link from a computer still in your devices. A computer that has only opened codeaf and whose chats contain only startup headers needs no `--replace`; saved knowledge, automations, tracked work and unknown local files still require the explicit replacement road. An active, already-paired computer is refused with the names of its other devices; `codeaf devices` lists them.

## Pair again in the chat — replace confirmation keeps local chats

On a removed computer, `/pair` accepts a code from `/pair` on a computer still
in your devices. It uses the same confirmation as `/pair <code> --replace`:
`Pair with your other devices? This computer's existing chats stay here but stop syncing; shared memories and settings are cleared. Provider keys saved on this computer stay here.`
Only `y` continues; `n` or `esc` keeps the existing chats. The retained local
chats no longer sync under the replaced membership.

## The link did not work — a pairing link or token that ran out, was declined or was answered already

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

These are the sentences a link pairing says when it stops, in the words the screen uses. Nothing was changed on either device when you read one of them.

- `that is not a codeaf pairing link or code; it looks like https://codeaf.agentfield.ai/p/k7m2q9xd#... or k7m2q9xd.<key>`: what was pasted is neither shape. Paste the whole link or the whole `<code>.<key>` token. Links from your own sync service are accepted too.
- `the link ran out before anyone approved it; run `codeaf pair` again for a new one`: a link is good for 10 minutes.
- `that link is not waiting any more; run `codeaf pair` on the new device for a fresh one`: the link’s own sync service said it is gone or expired. A link on a different sync service is refused before any request is sent.
- `that request was already answered`: another of your devices approved or denied it first.
- `the request was declined on your other device; run `codeaf pair` to ask again`: the new device sees this when you decline.
- `the check number does not fit that device, so nothing was approved`: the number on the approve screen does not belong to the device that asked. Do not approve; ask again.
- `open codeaf on this computer once, then approve again`: this computer has never started codeaf, so there is no local identity to share yet. Start codeaf here once, then approve the other computer.
- `paired, but your devices could not be read yet; open codeaf again in a moment`: the pairing worked; the device list needs a moment.

Too many requests from one network are refused too: at most 10 new links in an hour and 3 waiting at once from one network. See *Pairing your chats with a second computer* for the code route and its own limit `too many pairings from this network; wait 7 min`.

## I quit on my laptop and opened my desktop after pairing

After a clean quit and final durable save, bare `codeaf` on another paired computer within 24 hours opens the released chat automatically. A sleeping or disconnected laptop still needs `alt+c` on Home. Local unsaved edits, another open local window, a named chat, `--host`, `--at` or `--session` prevent automatic continuation. A running chat never moves by itself. The arrival retains its `Moved from` banner and `not brought along:` facts; `esc` goes Home.

Personal memory and stored model, display, approval and limit defaults follow the paired computers too. Limits are shared as values; spending counted against them stays per computer. Automations and machine-specific paths and settings stay local.

## An approval was interrupted — retrying the same pairing decision

A new computer receives its sealed pairing grant only after its approval has
been saved. If the reply is interrupted, retrying the same approval keeps the
same decision. A different answer cannot replace a decision already made. If
the link has run out, ask the new computer for a fresh one. Removing a device
blocks its later requests; it cannot take back bytes the device already received.

## After removal — does this machine keep syncing while its window is open

Once codeaf learns that this machine was removed, it stops asking for chats and stops sending updates. That refusal is remembered across windows and restarts. Your window stays usable for local chats. Home removes its offer to continue a chat from your other devices, its continue chord and the continue action on chat rows. The removal explanation stays visible.

Run `codeaf pair` to pair it again. Pairing can still reach the service and gives this machine a new device key. An already open Home then shows your other devices again.

## Terminal device list — sync service, this computer, platform and duplicate names

`codeaf devices` begins with `sync service: codeaf.agentfield.ai (hosted)` or `sync service: 127.0.0.1:8901 (your own)`. It lists this computer as well as the other paired computers. A known platform and online state or last-seen time appear beside each name; unknown details are omitted. Removed devices come last under `removed`.

Duplicate names use the same short suffix as the chat picker, for example `desk-b #8381`. Remove that one with `codeaf devices revoke desk-b#8381` or `codeaf devices revoke "desk-b #8381"`. An ambiguous name lists the choices and an example command. Removal cannot erase anything already on that computer.


## Pair from Home or after the second computer — /pair and a link field

`/devices`, `/pair` and `/settings` from Home open their card or page without
creating a conversation. The device list and pairing approval stay over Home.

`/pair` and Settings → Devices → Add another device offer both directions:
`paste the link the new device shows, or on it run codeaf pair 43-209-648`.
The command includes `--via` when this computer uses your own sync service.
The card also shows `> Paste pairing link`: paste or type the link there and
press Enter. Closing the card cancels its code; submitting a link switches to
the same approval card as Home, with the same check number and `a` / `d` keys.
A link for a different sync service is refused before looking it up. Your chat
draft stays separate. `c copy code` works until you start editing the link.


## Device removal when a list cannot be read or selectors collide

`codeaf devices revoke <name#tail>` checks both the devices that sync your chats
and the devices that can use this machine. If either list cannot be read,
`could not read the devices that sync your chats; nothing was removed — try again`
or `could not read the devices that can use this machine; nothing was removed — try again`
is the refusal. No removal happens. A selector that matches multiple devices,
even across the two lists, is refused with their available selectors.

## This computer before pairing — terminal devices list and sync off

Before pairing, `codeaf devices` lists this computer's local name, known platform
and `this device`, then says `Run codeaf pair to add another device.`
A sync list with no removable peer also ends with that pairing next step.
If this build cannot move chats, the local row is followed by the existing
remote-access instructions for `codeaf serve` and `codeaf chat --at <name>`;
it does not recommend unavailable chat pairing.
With `CODEAF_SYNC_URL=off`, neither the terminal nor the device cards draws a
`sync service:` line. The local row remains available.

## Pair again with the same devices after removal

On a removed computer, enter a current device's code in `/pair` and confirm the
replacement. Approval installs a fresh certified device key even when the
shared account is unchanged. The fresh computer appears as active in your
devices; the old removed entry stays removed. Local chats stay here, shared
memories and settings are cleared, and locally saved provider keys remain.


## Sync is off — which address should I set

A command that needs sync says
`sync is off: set CODEAF_SYNC_URL to your sync service's address`.
Set a service URL to enable syncing again. Device cards omit their service line
while syncing is explicitly off; their local computer row still appears.

## Pairing links with differently written service URLs

Pairing compares service URLs with scheme and host case ignored, default
ports omitted (`:443` for HTTPS, `:80` for HTTP) and trailing slashes omitted.
An actual difference in scheme, port, host or path prefix remains a different
service. A link for a different service is refused before any lookup there.
