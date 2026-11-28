# Adding a computer to your devices, approving it, and removing it

## How do I add another computer — the Add another device card (add another machine), codeaf pair, a link to paste, and pairing a server with no browser

While hosted relay storage is off, a chat still moves by blind pass-through between your paired computers when both are running codeaf; see *Blind pass-through*.

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. The original files stay untouched and remain usable in older codeaf builds.

A computer with submitted chats and its own pairing keys refuses ordinary
pairing without changing its chats. Use `--replace` to join the other set after
confirming: existing chats stay here but stop syncing, shared memories and
settings clear, and local provider keys stay here. A startup-only computer can
pair directly. A home that only completed desktop setup and installed the bundled
Canvas skill can still join. Startup lock files, empty app/service/factory folders and the
factory migration marker do not count as local work. Choosing a relay and
saving its storage status also leave an unused home eligible to join. Credential history is
accepted only when it has the known digest-only metadata format; actual keys,
unknown content and nonempty work folders remain protected.

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

## Add a device through your own relay — pairing instructions include --via

When this computer uses your own relay, step 1 includes its address: `1. On the new device, install and run: codeaf pair --via http://127.0.0.1:8901`. `/pair`, Settings → Devices → Add another device, and `codeaf pair --code` include the same `--via` argument.

Approving a self-hosted page's link in a terminal keeps that page's service when it matches this computer's selected service. A link waiting on a different service is refused before its code is sent.

The computer showing a numeric pairing code saves its selected relay when
it approves the joining computer, just before sending the identity. Showing or
cancelling a code alone does not change the saved service.

The card goes away once you have two devices. After that, `/pair` or Settings → Devices → Add another device still offers a place to paste the new device’s link, alongside the code command. It is not drawn when this chat cannot pair at all, for example a chat that runs over `--host` or `--at`.

## Do older chats move after pairing?

Hosted relay storage is off by default while its privacy terms are being finished. Pairing alone does not enable it: saved keys, preferences, shared memories, teams, idle handoff and task copies wait. Chats and their files do not wait: they move by blind pass-through between your paired computers while both are running codeaf (*Blind pass-through*). Pairing, device presence, remote control (once it is turned on) and live `--at` still work. After pairing, press enter on **Settings → Devices → relay storage** to ask for approval. A self-hosted relay with `--store` needs no request.

Older chats move through an independent copy. Once paired, the terminal chat prepares chats active in the last 30 days in the background while it is open, one at a time; the desktop app does not. Opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds. While hosted relay storage is off, that background preparation sends nothing to the relay: an older chat that is not open on its computer cannot move to another one until it is opened there (*Blind pass-through*) or relay storage is approved.

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

The approving device says `desk-b is now paired. desk-b can also see and control your chats here. Change that in /devices.`

## What does codeaf pair --code do

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

`codeaf pair --code` shows a code such as `04-063-213` on this computer. On the other computer, run `codeaf pair <code>` to share this computer's chats. Compare the three words and approve on the computer showing the code. `codeaf pair approve --code` is also accepted for showing a code. The page *Pairing your chats with a second computer* explains each step.

## The pair words — approve, --code and --via

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

```
usage: codeaf pair [--name <name>] | codeaf pair approve <link-or-token> | codeaf pair <code> [--replace] [--name <name>] | codeaf pair --code
```

- `codeaf pair` with nothing after it is for the NEW device. It asks to join and shows a link.
- `codeaf pair approve <link-or-token>` is for a device that is already in. It shows who is asking and the check number, then asks on its own line: `a approve · d decline:`. `a`, `y` and `yes` approve, without regard to case; `d`, Esc, Enter and unrecognised input decline. Pasting the link into the chat, or typing `/pair <link>`, does the same thing on a screen.
- `--name <name>` is what your devices call this computer. See the page *Naming a device*.
- `--via <address>` names the address of the relay to go through when it is not the one this device already uses. Pass it to both `codeaf pair` and `codeaf pair approve`. The new device prints the full approve line with `--via` in it when one is needed.

With a self-hosted address (`CODEAF_SYNC_URL=<url>` or `--via <address>`), the link opens that relay’s own `/p/<code>` page, and the terminal approval token includes `--via <address>`. It never prints a hosted page link for that invitation. The page shows the token command and an app link that keep the same address; the key stays after `#`. An address without a web page uses only the terminal token. Pasting a self-hosted link into a chat configured for another address sends no request and says `that link is waiting on codeaf.agentfield.ai, but this computer syncs through 127.0.0.1:8901 — on the new device run: codeaf pair --via http://127.0.0.1:8901`.

A link is good for 10 minutes and one approval. If it ran out, run `codeaf pair` on the new device again. Ctrl+c while it waits ends with `Cancelled. Nothing was paired.`

## I only opened codeaf and quit — can this computer pair without --replace

Yes. Skipping setup and quitting an empty chat counts as starting codeaf, with either a pairing code or a link. Its empty transcript, draft, command history, inventory and empty working folder do not require `--replace`.

Once you send a message, that chat belongs to this computer. With one such chat, pairing says `this computer already has 1 chat of its own, so nothing was changed.` The rest of the message explains `--replace`: local chats stay here but stop syncing, and shared memories and settings are cleared. Saved knowledge, automations, project work, chat memories and unknown local files keep their existing protection.

## The desktop app says this computer has local work — archived chats, backup and one shared library

Archiving a chat in WorkAF keeps it in the data folder, so it still counts as local work. If a computer was removed from the same shared library, pairing from a computer still connected to that library can bring it back with its saved work.

If the two computers have separate libraries, choose which one to share. Open WorkAF on that computer and show the code there. On the computer joining it, quit WorkAF and rename its data folder to keep a local backup. The default is `~/.workaf`; if you chose a custom data folder, set that folder aside instead. Reopen WorkAF using the original folder path and enter the code. Both computers then use the chosen shared library. The other library is available in the backup folder as local data.

## The four-digit check — how I know the right computer is asking

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

The new device shows a check number of four digits. The device that approves shows the same four digits. Look at both screens. Approve only when they match. If they do not match, decline: somebody else may have used the link.

On the approving screen the number reads `Check number 4821. Approve only if the new device shows the same number.` In a terminal the question reads the same, with the keys on the line below: `a approve · d decline:`. `a`, `y` and `yes` approve, without regard to case; `d`, Esc, Enter and unrecognised input decline. Code pairing keeps its `y / n` question.

## Approve a new device — the approve screen, a and d, and decline

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

On an already paired device, paste a link or type `/pair <link>` to see who is asking:

```
A new device wants to pair.
  △ laptop · Linux
  asked just now · 9 min left
Check number 4821. Approve only if the new device shows the same number.
laptop can also see and control your chats here. Change that in /devices.
a approve · d decline · esc later
```

- `a` lets it in. Press nothing else: **enter does nothing here**, because a device that is let in can read your chats.
- `d` declines it. The screen says `laptop was declined.`
- `esc` closes the screen without deciding. The request stays open until it runs out, and the same link opens it again.

The line under the name says what system the new device runs (Mac, Linux, Windows, iPhone or iPad, Android, or unknown system), when it asked, and how many minutes are left. A request that ran out says `run out`, and the screen says `that request has run out — ask the new device for a new link`.

While your answer is on its way the screen says `approving…` or `declining…` and offers no keys but `esc close`. If sync does not answer in time, codeaf looks the request up again. When your approval did arrive, the screen closes as approved. When it did not, it says `sync did not answer in time — try again`, and `a` and `d` work again. When codeaf cannot tell whether an approval arrived, it says `sync did not answer in time — your answer may still have arrived; check /devices before answering again`. A decline that times out says `sync did not answer in time — try again`, because a declined device never appears in `/devices`. Opening a link whose device you already let in says `that device is already paired — there is nothing left to approve`. If looking up the request times out it says `sync did not answer in time — try again`. When the request has run out, `a` and `d` are not offered at all.

When you approve, your screen says:

```
laptop is now paired. laptop can also see and control your chats here. Change that in /devices.
```

Every other device that is on says it too, a moment later, from the same message. In a terminal approval the line is `laptop is now paired. laptop can also see and control your chats here. Change that in /devices.` and a no says `Declined. Nothing was paired.` The terminal first says `laptop (Linux) wants to pair.` (the system is left out when it is not known). `codeaf pair approve` with nothing after it says `usage: codeaf pair approve <link-or-token> — the link or token the new device shows; for a numeric pairing code use codeaf pair <code>`.

A computer paired from now on can also see and control your chats here, and one paired earlier can only watch until you allow it; both are changed in `/devices` or with `codeaf devices permit <name> [--watch on|off] [--drive on|off]`.

## What does the dot next to a computer mean — the devices row, online and offline

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

When you have two or more devices, home shows one row of them:

```
● This Mac  ● tower  ○ desk (offline)
```

The plain repertoire uses a filled green dot `●` for online and a hollow dim dot `○` for offline. ASCII uses `#` and `o`; a patched font uses the same presence slots as `/devices`. Your own computer is filled while the strip is shown. A hollow dim dot `○` with `(offline)` is one that is not online: its lid is closed or codeaf is not running there. A computer counts as online only while it keeps answering: when another computer has not answered for 25 seconds (it is asleep, frozen, switched off or disconnected) its dot goes hollow and reads `(offline)`, within 30 seconds of its last answer, and a frozen computer no longer shows as online for minutes. It is back at once when it answers again. The row changes by itself, with no key to press, when a device comes or goes. As a backstop, home also reads the list again every 5 minutes while it is connected, and every minute while it is not. If the relay has not answered for 25 seconds, Home cannot tell who is online and hides the strip. A stalled connection is subscribed again, so a late answer cannot keep an old online set alive. The strip returns with fresh presence; it never guesses that a silent service means every computer is offline.

Under the row, `alt+m move here` brings the newest chat of an online device to this one. With more than one choice it asks `Which device?` and the last choice is `leave it there`. If there is nothing to bring it says `no other device that is online has a chat to move here`. The page *Continuing a chat on another device* tells what happens next.

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
relay: 127.0.0.1:8901 (your own)
  ● This Mac  Mac  this device
› ● tower  Linux
  ○ desk  Linux  seen 3h ago
  Add another device
↑↓ choose · n rename · r remove · esc close
```

A filled dot is online, a hollow one is not, and an offline device says when it was last on (`seen 3h ago`), or just `offline` when it was never seen. This computer is listed even before pairing, so `n rename` works on its row. `Add another device` follows the computers; select it and press `enter` to open `/pair`. On that row the keys are `↑↓ choose · enter add · esc close`. `r remove` shows only on a row that can be removed, and `↑↓ choose` only when there is another row.

## Remove a computer — r asks once, y removes, any other key keeps it

The removable row’s key line says `r remove`. To remove a device or phone (the command line calls this revoking: `codeaf devices revoke <name>`), move to its row and press `r`. The card asks `remove desk? it must pair again to come back  ·  y removes  ·  any other key keeps it`. It uses the shown name, including a duplicate name’s `#tail`. Only `y` removes it; every other key, including `esc`, cancels and keeps that row selected without doing anything else. After `y`, the row says `removed`:

```
desk was removed — it can no longer reach your chats.
```

Removing a phone stops every one of your computers serving it, clears whatever was waiting for it, and each of them remembers the removal, so an older copy of the device list cannot let it back in.

## Add a phone

To connect the codeaf phone app instead of another computer, open `/devices` (or `Settings → Devices`) and press `a` (`a add a phone`).

- When sync is on, `/devices` shows the setup link to open on your phone:
  - `add a phone: open this link on your phone, then choose a computer in the app:`
  - `workaf://setup?v=1&sync=<url-encoded-sync-url>`
  - `The phone then shows its own link; paste it here with /pair.`
- When sync is off (`CODEAF_SYNC_URL=off`), pressing `a` shows no link and says:
  `sync is off on this computer, so a phone cannot connect yet.`

After you open the setup link on your phone, the phone shows its own 10-character pairing link (`<code>.<key>`) and a four-digit check number. Paste that link into `/pair <link>` on your computer (or run `codeaf pair approve <link-or-token> [--check <n>]` in a terminal). The approval card says:

- `A phone wants to connect.`
- `It can start chats and answer on all your computers. It never gets your keys or your files.`
- `Check number 4821. Approve only if the phone shows the same number.`

Press `a` to approve once the four-digit check number matches your phone. When approval finishes, codeaf says:

`iPhone is now paired as a phone. It can start chats and answer on every computer you have paired. Remove it in /devices.`

In `/devices` and `codeaf devices`, a paired phone is listed after your computers with system `phone` and access `starts chats, answers`. Pressing `w` or `t` on a phone row (or running `codeaf devices permit` on a phone) says `this is set on the phone`. See *Your phone* (`your-phone`) for what a phone can do and how to start a chat on a computer from your phone.

## Removed device rows, list order and a missing computer

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
## Devices in /settings — where your computers are on the settings page, add another device, rename this one, or remove (revoke) one from settings

Open `/settings` (or `ctrl+,`) and walk with `←` and `→` to `Devices`, between `Connections` and `Privacy` — or type `devices`, `remove`, `revoke`, `pair` or `add device` in settings search.

While loading, the tab says `looking up your devices…`. Loaded, it begins with the same dim `relay: 127.0.0.1:8901 (your own)` or `relay: codeaf.agentfield.ai (hosted)` line as `/devices` and the terminal. It lists this computer even before pairing, with the same platform and presence columns and in the same order and words as `/devices` — `●` online, `○` away (`seen 3h ago`, or `offline`), `this device`, and `removed` — followed by `Add another device`. The open tab refreshes on its own when a computer joins, renames itself or is removed.

## Keys in Settings Devices — add, rename and confirm removal

In `/settings` → Devices, the key line shrinks to what works on the row (`↑↓ choose` when there is more than one row, `r remove` only on a removable computer, `←→ categories`, `esc close`):

- `enter add` on `Add another device` leaves `/settings` and starts `/pair`.
- `n rename` on `this device` opens `name this device: ` under the row with `enter save · ctrl+u clear · esc cancel`. While typing, `esc` cancels the edit without closing `/settings`; a second `esc` leaves `/settings`. On another row `n` says `a device renames itself — choose this device's row, or open /devices on the one you mean.`
- `r remove` (called `codeaf devices revoke <name>` on the CLI) asks `remove desk? it must pair again to come back  ·  y removes  ·  any other key keeps it`. Only `y` removes it, marks its row `removed`, and says `desk was removed — it can no longer reach your chats.` Any other key cancels, keeps the same row selected, and does nothing else. On your own row `r` says `this device cannot remove itself — choose another row.` On `Add another device` or a `removed` row, `r` does nothing.

With remote control on, on `/devices` and here, another computer's row also says what it may do with the chats on THIS computer, `watch and type`, `watch only` or `not shared`, and the key line gains `w watch` and `t type`: `↑↓ choose · n rename · r remove · w watch · t type · esc close`. `w` turns watching on or off (off turns typing off too); `t` turns typing off at once, and on only after `let desk type into chats on this computer? y turns it on, any other key keeps it off`. One line follows: `desk can watch chats on this computer.`, `desk can no longer watch chats on this computer.`, `desk can type into chats on this computer.` or `desk can watch, not type.` A build that cannot change it says `this build cannot change that`. Your own row, `Add another device` and a removed row have none of this, and with remote control off the rows are as above.

Where devices cannot work, the tab shows one sentence and no rows: `pairing is not available here` (a chat over `--host` or `--at`), `moving chats needs a newer furrow engine than this build carries`, or the read error. With `CODEAF_SYNC_URL=off` it lists this computer, and `enter add` answers `sync is off (CODEAF_SYNC_URL=off); pairing needs the relay`, as `/pair` does.

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

Pairing again restores device access. On the hosted relay, sending keys or shared state needs relay storage approval (Settings → Devices → relay storage); chats move by blind pass-through while both computers are running codeaf.

`/devices` on a removed computer says the removal notice in place of the list. A terminal refusal uses `this device was removed by another of your devices, so this chat stays here only — run codeaf pair --replace to bring it back`. Home says `removed by another of your devices — pair again to sync`. The terminal command `codeaf devices` gives its removal explanation once, without following it with `nothing is paired yet`. Removal appears immediately in an open window and on Home, and after restarting, once only. Your local chats stay usable while this computer is removed, and no longer follow your other devices.

In the desktop app, choose Pair a computer and enter a current device's code or ask to join by link. After approval from a device still in your devices, this computer keeps the same identity, its local chats, shared memories and settings, and receives a fresh device key. Its chats can sync again. No replacement confirmation is needed. A computer that was never removed is told `This computer is already paired with <name>.` before the other computer is asked, and nothing changes.

The terminal command still requires `codeaf pair --replace` on a removed computer. This is explicit replacement, even with the same identity: existing chats are moved aside and stop syncing, shared memories and settings are cleared, and locally saved provider keys remain. Approve the link from a device still in your devices. An active, already-paired computer is refused with the names of its other devices; `codeaf devices` lists them.

## Pair again in the chat — replace confirmation keeps local chats

On a removed computer, `/pair` accepts a code from `/pair` on a computer still
in your devices. It uses the same confirmation as `/pair <code> --replace`:
`Pair with your other devices? This computer's existing chats stay here but stop syncing; shared memories and settings are cleared. Provider keys saved on this computer stay here.`
Only `y` continues; `n` or `esc` keeps the existing chats. The retained local
chats no longer sync under the replaced membership.

## The link did not work — a pairing link or token that ran out, was declined or was answered already

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

These are the screen's sentences when link pairing stops. Nothing was changed on either device when you read one of them.

- `that is not a codeaf pairing link or code; it looks like https://codeaf.agentfield.ai/p/k7m2q9xd#... or k7m2q9xd.<key>`: what was pasted is neither shape. Paste the whole link or the whole `<code>.<key>` token. Links from your own relay are accepted too.
- `the link ran out before anyone approved it; run `codeaf pair` again for a new one`: a link is good for 10 minutes.
- `that link is not waiting any more; run `codeaf pair` on the new device for a fresh one`: the link’s own relay said it is gone or expired. A link on a different relay is refused before any request is sent.
- `that request was already answered`: another of your devices approved or denied it first.
- `the request was declined on your other device; run `codeaf pair` to ask again`: the new device sees this when you decline.
- `the check number does not fit that device, so nothing was approved`: the number on the approve screen does not belong to the device that asked. Do not approve; ask again.
- `open codeaf on this computer once, then approve again`: this computer has never started codeaf, so there is no local identity to share yet. Start codeaf here once, then approve the other computer.
- `paired, but your devices could not be read yet; open codeaf again in a moment`: the pairing worked; the device list needs a moment.

Too many requests from one network are refused too: at most 10 new links in an hour and 3 waiting at once from one network. See *Pairing your chats with a second computer* for the code route and its own limit `too many pairings from this network; wait 7 min`.

## I quit on my laptop and opened my desktop after pairing

While hosted relay storage is off, `idle handoff` does not run, and a laptop where codeaf has quit cannot hand the chat over: the desktop can take it only while codeaf is running on the laptop. See *Blind pass-through*.

After a clean quit and final durable save, bare `codeaf` on another paired computer within 24 hours opens the released chat automatically. By default (**Settings → General → idle handoff** is off), open idle chats keep their hold and selecting a released chat on Home asks **Move here**. Turn on **idle handoff** in `/settings` (or set `CODEAF_IDLE_HANDOFF=1`) so an idle paired chat lets go about 2 seconds after it goes idle once saved to the relay, and Enter on Home (or `agents.open` in the app) opens a released chat directly. A lapsed hold on another computer still asks Move here, including when its holder was working or its last working state is unknown. A laptop that went to sleep or disconnected while still holding the chat needs `alt+c` on Home while its hold is live. Local unsaved edits, another open local window, a named chat, `--host`, `--at` or `--session` prevent automatic continuation. A running chat never moves by itself. The arrival retains its `Moved from` banner and `not brought along:` facts; `esc` goes Home.

Personal memory and stored model, display, approval and limit defaults follow the paired computers too. Limits are shared as values; spending counted against them stays per computer. Automations and machine-specific paths and settings stay local.

## An approval was interrupted — retrying the same pairing decision

A new computer receives its sealed pairing grant only after its approval has
been saved. If the reply is interrupted, retrying the same approval keeps the
same decision. A different answer cannot replace a decision already made. If
the link has run out, ask the new computer for a fresh one. Removing a device
blocks its later requests; it cannot take back bytes the device already received.

## After removal — does this machine keep syncing while its window is open

Once codeaf learns that this machine was removed, it stops asking for chats and stops sending updates. That refusal is remembered across windows and restarts. Your window stays usable for local chats. Home removes its offer to continue a chat from your other devices, its continue chord and the move action on chat rows. The removal explanation stays visible.

Run `codeaf pair` to pair it again. Pairing can still reach the service and gives this machine a new device key. An already open Home then shows your other devices again.

## Terminal device list — relay, this computer, platform and duplicate names

`codeaf devices` begins with `relay: codeaf.agentfield.ai (hosted)` or `relay: 127.0.0.1:8901 (your own)`. It lists this computer as well as the other paired computers. A known platform and online state or last-seen time appear beside each name; unknown details are omitted. Removed devices come last under `removed`.

Duplicate names use the same short suffix as the chat picker, for example `desk-b #8381`. Remove that one with `codeaf devices revoke desk-b#8381` or `codeaf devices revoke "desk-b #8381"`. An ambiguous name lists the choices and an example command. Removal cannot erase anything already on that computer.


## Pair from Home or after the second computer — /pair and a link field

`/devices`, `/pair` and `/settings` from Home open their card or page without
creating a conversation. The device list and pairing approval stay over Home.

`/pair` and Settings → Devices → Add another device offer both directions:
`paste the link the new device shows, or on it run codeaf pair 43-209-648`.
The command includes `--via` when this computer uses your own relay.
The card also shows `> Paste pairing link`: paste or type the link there and
press Enter. Closing the card cancels its code; submitting a link switches to
the same approval card as Home, with the same check number and `a` / `d` keys.
A link for a different relay is refused before looking it up. Your chat
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
`relay:` line. The local row remains available.

## Pair again with the same devices after removal

On a removed computer, enter a current device's code in `/pair` and confirm the
replacement. Approval installs a fresh certified device key even when the
shared account is unchanged. The fresh computer appears as active in your
devices; the old removed entry stays removed. Local chats stay here, shared
memories and settings are cleared, and locally saved provider keys remain.


## Sync is off — which address should I set

A command that needs sync says
`sync is off: set CODEAF_SYNC_URL to your relay's address`.
Set a service URL to enable syncing again. Device cards omit their service line
while syncing is explicitly off; their local computer row still appears.

## Pairing links with differently written service URLs

Pairing compares service URLs with scheme and host case ignored, default
ports omitted (`:443` for HTTPS, `:80` for HTTP) and trailing slashes omitted.
An actual difference in scheme, port, host or path prefix remains a different
service. A link for a different service is refused before any lookup there.

## The first /status after pairing — relay storage on your own service

Successful pairing asks the relay for your account's storage permission and saves its answer on this computer. It makes up to three attempts inside about 1.5 seconds, so a temporary startup refusal need not lose the answer. This applies to `codeaf pair`, `codeaf pair approve`, a link pasted on Home and `/pair`. An already-open window's next `/status` shows `relay storage  off` when that answer is off, including with your own service address. A service that does not report storage permission adds no terminal line but its successful answer is saved; an unavailable service leaves the previous saved answer alone.

The desktop's pairing receipt confirms that the computers paired. It does not
promise that chats can continue elsewhere while storage is off. A refusal
because this computer has its own work offers **Done**, rather than **Try again**.

A link join names the other computer only when it is the sole other active
device. With several possible approvers it uses the generic pairing receipt;
the directory's alphabetical order does not identify who approved the link.

## Saved provider keys added after pairing

With storage on, a provider key added or disconnected in the desktop is shared
through the encrypted vault after pairing too. A remote vault change wakes the
receiving desktop through its existing directory watch. Received keys go through
the app's credential handoff to its secure store, and refresh its open provider
list; key values do not ride change events or enter plaintext profile files.
Unrelated credentials such as GitHub tokens and keys inherited only from the
shell are not added to this provider-key carrier. Custom providers need their
own endpoint configured on the receiving computer before a shared key applies.
Keys entered while storage is off wait locally and share after approval.
