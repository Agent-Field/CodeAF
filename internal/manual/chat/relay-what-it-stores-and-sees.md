# What the relay stores and what it can see

## Turn on relay storage (request access) — why don't my chats sync to my other computer

Hosted relay storage is off by default while its privacy terms are being finished. Pairing does not turn it on. New chats, files and saved keys stay on this computer; the relay refuses even encrypted uploads. Existing stored frames stay readable and nothing is deleted.

Pairing, seeing your devices and `--at` still work while the other computer is on. With storage off, new chat turns cannot be continued on another computer while the first is off, recovered from the relay after losing a computer, or sent as task copies. Teams sharing, synced keys, preferences and shared memories also wait. The relay still keeps device and directory coordination metadata, including existing chat records and leases; storage off does not erase those records.

After pairing, open **Settings → Devices → relay storage**. When it says **off**, press **enter** (`enter request access`). This opens a pre-filled GitHub issue; the panel also shows the request code and link if no browser opens. The request code is your identity id (`id_` followed by 32 hex digits). It is safe to post publicly and grants nothing by itself.

Serving processes and chat windows check at launch, after `/status` in chat, and every ten minutes while hosted storage is off. Once approval is read, waiting chats and uploads start within a minute without a restart. Opening a chat never waits for a relay reading. `/status` shows the dim line `relay storage  off` while hosted storage is off. Revocation stops new uploads and leaves earlier frames readable.

A self-hosted `codeaf relay --store <directory>` keeps storage on and needs no request. Approved hosted accounts still have the limits on *Relay limits and refusals*.

## Does the relay store my chats — what is on the relay's disk

Hosted relay storage is off by default while its privacy terms are being finished; request access in Settings → Devices → relay storage → enter. Pairing and live `--at` work without storage. With storage enabled, the relay holds two things for each identity, kept apart from every other identity: a **directory** (one record for each chat, one for each of your computers) and a **store** of chat content.

The content is in the store as **encrypted chunks**. Each chunk is sealed under a key that only your own computers have. The relay cannot open a chunk. It keeps the chunks you send, and your saved keys too: they travel as one more sealed chunk, like a chat.

Two things in the directory are sealed before they leave your computer: **the title of every chat** and **the name of every computer**. The relay stores them as sealed text and cannot read them.

There is no email or password sign-in on the relay. Storage approval is tied to your identity id. Your identity is an id that starts with `id_`, made from your key.

## What can the relay see — the metadata, my IP address, and what is not hidden from it

**The relay cannot read:** your messages, your files, the titles of your chats, the names of your computers, your saved keys, the pairing code, the three words.

**The relay can see the following metadata. Directory and coordination records can remain while storage is off; chunk metadata applies to content already stored or uploaded with storage on:**

- your identity id, and the id of each computer you use (an id, never a name);
- for each chat: its id, how big it is, when it last saved, which computer holds it, and the chat it was split from;
- for each computer: its operating system, processor type, and whether it has a sandbox or a copy-on-write disk;
- for each chunk: its size, and the ids of the parts inside it;
- when you connect, how often, and how much you send;
- the network address each request came from.
- coordination metadata: cluster and member ids, roles, term and log position, learner progress, request sequence and digest;
- worker routing: opaque assignment id and revision, selected chat and worker ids, process number, ownership number, status and times.

The hosted relay has request logs and traces disabled. Rate counters retain network information for their fixed windows. A relay you run yourself logs only the first 8 characters of the identity id, the kind of request, the status and two byte counts. It never logs a body. An operator of a relay you do not run can do more than that; you cannot check it.

The relay can also delete what it holds, or refuse to serve it. Keep a copy you care about on a computer of your own.

## Does the relay see my code, my prompts or my files

Hosted storage is off until approved; no new chat or file chunks are kept while off. With storage on, turns, tool output and files are sealed on your computer before they are sent. The relay gets ciphertext and sizes.

Your saved keys are sealed too, as said above.

Continuing a chat on another computer brings its saved workspace files with its conversation, tool output and task journals. Rebuildable install folders and withheld files are listed on the arrival card. Secret files travel separately in the sealed vault between paired devices. See *Continuing a chat that one of your machines holds* and "What comes with a continued chat" on *Home*. It does not keep two arbitrary project folders mirrored while you edit them.

## Optional warm-transfer frame hints and relay compatibility

The warm-transfer prototype can attach up to two encrypted chunk ids and their
encoded byte sizes to a saved chat head. The total, including one extra read byte
per chunk, is at most 64 KiB. These are readable transfer hints, never proof that
all content arrived. Invalid hints are ignored; saving the chat still succeeds.
They appear on a single-chat read and stay off Home's listing. Each publish
replaces them, and a publish without hints clears the previous hints.

The prototype is off by default. Every member of the relay cluster must run a
build that understands these fields before any computer sends them. Older members
refuse unknown fields; existing requests omit the fields and keep their encoding.
