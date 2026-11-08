# Locking out a lost computer for good

## A computer was stolen and I want it shut for good — codeaf identity rotate gives the computers you keep a replacement identity

Hosted chat/file sync requires storage approval in Settings → Devices → relay storage → enter; pairing alone does not enable it. Pairing, the device list and live `--at` still work while storage is off. A self-hosted relay with `--store` needs no request.

Stopping a computer with `codeaf devices revoke <name>` cuts it off from the relay. It does not undo what it holds. That computer has your whole identity. It can read everything it already has, and it can make itself a new device.

`codeaf identity rotate` is the real fix. It makes a replacement identity and moves your saved keys to it. With hosted relay storage off, rotation still finishes without uploading chats or resealed keys: chats stay on the computers that hold them. Previously stored frames remain under the old identity until grace-period erasure. The old identity is shut. The lost computer holds the old one, so it can read nothing of what you do from then on.

Run it on a computer you keep, as soon as you know a computer is lost:

```
codeaf identity rotate
```

It is hidden from the help text and, like the other `codeaf identity` verbs, is available by default on Linux, macOS and native Windows with a compatible furrow engine. Native Windows is supported too. It needs a relay. With sync off it says `a rotation needs a relay`.

```
usage: codeaf identity rotate [--grace 7d] [--yes] [--abandon]
```

## What a rotation does — the steps, in order

Hosted chat/file sync requires storage approval in Settings → Devices → relay storage → enter; pairing alone does not enable it. Pairing, the device list and live `--at` still work while storage is off. A self-hosted relay with `--store` needs no request.

1. It tells you the cost first, and asks. It says how many chats you have, how many are not on this computer, how many megabytes go up and about how long that takes. Answer `n`, or press ctrl+c, and nothing has changed.
2. It fetches the chats this computer does not hold yet. This can take a long time for big chats. You can still cancel.
3. It makes the new identity and writes it down on this computer, before it sends anything.
4. It shuts the old identity on the relay. Nobody can write to it any more, this computer included. Your other computers show that your chats are moving, and wait.
5. With storage on, it seals every chat and your saved keys again and uploads them under the new identity. Approval follows only after proof from the new identity; a successor’s own storage setting stays in place. With hosted storage off, no resealing uploads are made; chats stay on your computers and the saved keys are resealed locally.
6. With storage on, it checks that the relay has everything; with storage off, it proceeds with local copies.
7. This computer switches to the new identity. This is the step after which there is no way back.
8. It asks the relay to delete the old identity after a grace period. The default is 7 days. Use `--grace` to change it: the relay takes 1 hour to 30 days.

When it is done it says:

```
rotated. your chats now belong to id_… (was id_…).
other computers: run /pair here, then `codeaf pair <code>` there, for each one.
the old copy on the relay is read-only and is deleted on 2026-10-08.
a computer you lost still holds everything it had: your chats up to now and every secret in your vault.
change these at their providers: OPENROUTER_API_KEY
```

## The rotation stopped half way — resume it or abandon it

Hosted chat/file sync requires storage approval in Settings → Devices → relay storage → enter; pairing alone does not enable it. Pairing, the device list and live `--at` still work while storage is off. A self-hosted relay with `--store` needs no request.

If `codeaf identity rotate` stops half way, because the network dropped or the computer went to sleep, run it again. It finds what it had done and finishes. It never makes a second new identity. If saved replacement keys are damaged, keep `rotation-successor.json`; deleting it would lose the replacement already bound by the relay. While a rotation is unfinished, the computer does not sync, and says `a rotation is unfinished on this computer; run codeaf identity rotate to finish it`.

To stop a rotation before step 7, run `codeaf identity rotate --abandon`. The old identity opens again. Only after a hosted relay accepts the successor binding does this computer keep the replacement keys privately, outside the active rotation, for another rotation of the same identity on that same relay. Canceling before acceptance, using a relay without the storage capability, or using a self-hosted relay discards the candidate; the next rotation makes fresh keys. After step 7 the only way is forward: it says `already switched to the new identity`.

If the computer that started the rotation is lost too, run `codeaf identity rotate --abandon` on another computer that still has the old identity. It opens the old identity again.

## What a lost computer still keeps — be plain about it

A rotation does not reach into the lost computer. **It keeps everything it already downloaded:** every chat it has, every saved key in its vault, and the old copy of your chats on the relay until the relay deletes it, after the grace period. Copies of that old data that anyone kept, such as a backup, stay readable for ever, because the old keys open them.

After a rotation the lost computer cannot sync any more, cannot write to your chats, and cannot read anything you do from then on.

**Your provider keys are not changed.** An OpenRouter key or an Anthropic key is owned by the provider, not by codeaf. The lost computer still has the keys that were in your vault. Go to each provider and make a new key. The card at the end lists the names of the keys, never the values.

Remote access is a different door. A machine you let in with `codeaf serve` is not changed by a rotation. `codeaf devices` still lists it. Stop it there with `codeaf devices revoke <name>`.

## Pair your other computers again — each one follows the replacement

Hosted chat/file sync requires storage approval in Settings → Devices → relay storage → enter; pairing alone does not enable it. Pairing, the device list and live `--at` still work while storage is off. A self-hosted relay with `--store` needs no request.

After a rotation, each computer you keep is still on the old identity. It stops syncing, and says once: `your chats are moving to a new identity; when that is done, pair this computer again (/pair on the computer that moved them)`.

When the relay has deleted the old identity, after the grace period, it says:

```
your identity was replaced and the relay has deleted the old one; pair this computer again (/pair on a computer that has the new one)
```

On the computer you rotated on, type `/pair`. On the other computer, use the code: `codeaf pair <code>`, or `/pair <code>` in the chat. Compare the three words as always.

That computer follows the replacement. It does not need `--replace`. It keeps its own chats and its saved keys.

A computer that was never on your old identity is refused as before, and keeps its own chats.

## Who rotates first wins — the one risk

Every computer that holds your identity can run `codeaf identity rotate`, and so can a thief who holds it. The relay cannot tell you apart. The first one to shut the old identity wins.

So do it at once. If you run it and it says `this identity was already rotated; the new one is on the device that did it`, someone else got there first. The new identity is on that computer, not on yours.

If a rotation of the identity is already under way and this computer has no record of it, it says:

```
a rotation of this identity is already under way; if it is yours and was cut off, run codeaf identity rotate --abandon, otherwise wait for the computer that started it
```

A relay that does not know how to do this says `this relay cannot replace an identity: it is too old for that`. Nothing has been fetched or changed when you read either sentence.

A way to prove your identity with a written recovery phrase is not built.

## What rotation costs — uploads with storage on, local copies with storage off

With hosted storage off, rotation completes with no store writes. Chats remain on your computers; any already-stored frames are left to the old identity’s grace-period erasure. With storage on, a rotation uploads everything your chats hold, once, because the new identity starts empty. The plan it shows before you answer gives the number. A chat of 200 MB goes up as 200 MB. The transfer takes your network bandwidth and uses relay storage; the old and new copies count towards its limits during the grace period. There is no fixed price or duration promised by the command.

While the old identity is kept for the grace period, the relay holds both copies.

Each computer you pair again sends what it has for a chat the first time it works on it, because it starts with an empty list of what the relay holds.
