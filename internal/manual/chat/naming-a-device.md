# Naming a device

## Why does it show my hostname — the host name is the default until a device is named, and where a name appears

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

Every screen that names a device uses one name for it: the home rows (`held on tower`, `tower offline`), `Continue tower's latest chat here?`, the approve screen, `tower is now paired. Your chats can continue there.`, `/devices`, `/settings` → `Devices`, and the line after a move, `Moved from tower in 4s`. A device is called by its host name, the word your operating system already uses for it, until you give it a name. A name is only a label. It is not a key and it signs nothing, and renaming a device changes nothing about what it can reach.

Because you never named this device, it kept the default. Name it and every other device shows the new name.

## How do I name a device when it joins — codeaf pair --name

On the new device, type `codeaf pair`. In a terminal it asks once, with the host name filled in:

```
Your other devices will call this computer "tower". Press Enter to keep that, or type another name:
```

Press Enter to keep the host name, or type a name and press Enter. To say it without being asked, type `codeaf pair --name "atlas"`. It works with a typed code too: `codeaf pair 42-715-302 --name "atlas"`. The device that approves shows that name when it asks you to approve.

Current computers include the platform during approval, such as `Abir (Windows)`,
so two computers named Abir can be distinguished.

## How do I rename a device later — codeaf devices rename and the n key

Type this on the device you mean:

```
codeaf devices rename "atlas"
```

Or open `/devices` (or `/settings` → `Devices`), stay on the row marked `this device`, and press `n`. The name is filled in under the list:

```
name this device: atlas▏
enter save · ctrl+u clear · esc cancel
```

Edit it and press enter to save, ctrl+u to clear it, esc to leave it as it was. A device renames itself, so `n` on any other row says `a device renames itself — choose this device's row, or open /devices on the one you mean.` To rename a device, rename it on that device.

After a rename it says:

```
This computer is now called atlas.
```

Your other devices show the new name the next time they list devices or draw a home row. If the sync service cannot be reached at that moment, the device keeps the name and says:

```
This computer is now called atlas. Your other computers will see it once this computer is back online.
```

You do not type it again. A running paired desktop engine retries automatically,
even without a chat. A device that is not paired with anything yet says
`This computer is now called atlas.`

## What can a device name be — length, spaces, control characters and duplicates

One to 32 characters you can see. Spaces at the start and end are dropped. A name that cannot be used is refused, not cut or repaired, with one of these:

- `a device name needs at least one visible character`
- `a device name is at most 32 characters`
- `a device name cannot hold control characters or line breaks`

Two devices may have the same name. `codeaf devices`, `/devices` and `/settings` → `Devices` then add a short tail, such as `tower #a1b2`, so you can tell them apart.
