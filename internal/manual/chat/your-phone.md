# Your phone

## Connect my phone — can I use codeaf on my phone, and how do I add a phone?

Yes. Pair the phone app once, at any one of your computers, and every computer you have paired serves it. The phone never holds your keys.

1. On a computer, open `/devices` (or `/settings` and then Devices) and press `a`, which is `a add a phone`. When sync is on it shows:
   - `add a phone: open this link on your phone, then choose a computer in the app:`
   - the setup link, which starts `workaf://setup?v=1&sync=` and names this computer's sync address
   - `The phone then shows its own link; paste it here with /pair.`
   When sync is off it says `sync is off on this computer, so a phone cannot connect yet.` and shows no link.
2. Open the setup link on the phone and choose a computer in the app. The phone shows its own pairing link: a 10-character code, a dot and a key, and a four-digit check number.
3. On the computer, paste that link or `code.key` into `/pair`. The card says `A phone wants to connect.`, then `It can start chats and answer on all your computers. It never gets your keys or your files.`, then `Check number` with four digits, and to approve only if the phone shows the same number.
4. Compare the number with the phone and press `a` to approve (`d` declines, `esc` leaves it for later). From a terminal without the screen, run `codeaf pair approve <link-or-token>`, or add `--check <n>` with the number the phone shows: a number that does not match approves nothing and says `that check number does not match. Nothing was approved.`

When it is approved codeaf says `<name> is now paired as a phone. It can start chats and answer on every computer you have paired. Remove it in /devices.`

If the link was copied without its key, codeaf says `that link has no key; copy the whole link from the phone`. If the request does not match its link it says `that request does not match its link. Nothing was approved.`

A computer that has never been paired with anything keeps quiet and makes no requests to the relay. Approving a phone lets it make only the requests that serving that phone needs, so one computer and a phone are enough. Removing the last phone makes that computer quiet again.

## Start a chat on a computer from my phone

Tap `+` in the phone app, pick which of your computers should run the chat, write the first message and send it. The chat starts on that computer and on no other.

- **The computer is on and codeaf is running there:** the chat starts at once and the reply appears on the phone as it is written.
- **The computer is off or asleep:** the request waits for up to one hour. If the computer comes back inside that hour the chat starts, once. After the hour the phone says the request expired and nothing starts.
- **The computer cannot do it:** the phone shows that the computer could not start the chat. Check that computer, then try again from the phone.

Other requests from the phone (a message to a chat, an answer, stopping a chat) wait up to a day, and only the computer that holds that chat runs them. A chat that has moved to another computer is refused, not run twice.

## What can my phone do?

A paired phone appears in `/devices` and in `codeaf devices` after your computers, with the word `phone` and `starts chats, answers`.

It can:

- start a chat on a computer you choose;
- read your chats and their latest replies on every paired computer;
- send a message to a chat, and stop one;
- answer the questions an agent asks while it works.

It cannot:

- answer a question that would do something that cannot be undone, such as deleting or publishing, unless you confirm with Face ID or your fingerprint on the phone. Without that the computer refuses it;
- be set to watch or type with `w` and `t` in `/devices`: on a phone row they say `this is set on the phone`, and `codeaf devices permit` on a phone says the same;
- lead the computers: a phone cannot be the coordinating computer;
- reach your keys, your files or your folders: they stay on your computers.

## My phone says a computer is offline

A computer answers your phone only while codeaf is running on it: the app, or `codeaf phone serve` in a terminal. Closing every codeaf window on a laptop leaves that laptop offline to the phone until you open the app again.

For a computer with no window open, such as a workstation or a server you keep awake, run `codeaf phone serve`. It says `serving your phone on this computer. ctrl+c stops it.` and keeps going until you stop it. If the app is already open on that computer it says `codeaf app is open on this computer and is already serving your phone.` and does nothing more.

If you run `codeaf phone serve` on a computer that has never been paired with a phone or another computer, it says `no phone is paired with this computer yet. Approve one first, then run this again.` and stops without sending anything.

While a computer is offline, a new chat for it waits up to one hour, as above.

## Does adding a phone share my chats between my computers?

No. Adding a phone does not start sharing chats between computers. A computer you have only paired with a phone stays on its own: its chats are not published for any other computer, there is no second computer to read them, and it does not take part in your other computers' sync. It sends the phone only what the phone shows, sealed so that only that phone can read it, and the requests it needs to find the phone and answer it.

Chats start moving between your computers when you pair a second computer with `/pair`, not when you add a phone. Approving a computer is what ends the "never paired" state, because that computer is given your keys. Approving a phone never does.

## My phone was lost or stolen

Remove it from any one of your paired computers:

- **On the screen:** open `/devices`, move to the phone, press `r`, then `y`. It asks `remove <name>? it must pair again to come back  ·  y removes  ·  any other key keeps it`, and then says `<name> was removed — it can no longer reach your chats.`
- **From a terminal:** `codeaf devices revoke <name>`.

Every one of your computers stops serving it, whatever was waiting for it is cleared, and your computers remember the removal, so an old copy of the device list cannot let it back in.

The phone never held your identity, your sync keys or your provider keys, so removing it is all there is to do. You do not need to rotate anything.

## Why does an older computer not accept my phone's link?

A phone's pairing link has a 10-character code, and a link from another computer has an 8-character one. A codeaf from before phones existed only knows the 8-character kind, so given a phone's link it says what it always said about a link it cannot read: `that is not a codeaf pairing link or code; it looks like https://codeaf.agentfield.ai/p/k7m2q9xd#... or k7m2q9xd.<key>`. That older build cannot be changed. Update codeaf on that computer and paste the phone's link again.

That is deliberate: a computer that cannot recognise a phone's link cannot mistake it for a computer and hand it your keys.

## The phone and your keys

The phone makes its own keys and keeps them. When you approve it, this computer gives it only a certificate for those keys, your identity's public key and this computer's signed card, sealed so that only the phone can read it. Your identity's secret, your sync keys and your provider keys are never sent to the phone, and the relay cannot read what passes between the phone and your computers.

If phone serving pauses with a message that the device list on this sync address is older than one this computer saw, the relay was probably reset. Chats and sync are untouched; only the phone is paused. Look at `/devices`, and if that is what happened, run `codeaf phone serve --accept-older-list`. It says `accepted the older device list. Phones paired before the reset must pair again.` and keeps serving.
