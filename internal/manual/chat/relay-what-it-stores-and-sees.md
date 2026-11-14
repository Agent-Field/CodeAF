# What the relay stores and what it can see

## Request access to sync storage — turn on sync storage, why don't my chats sync to my other computer

Hosted sync storage is off by default while its privacy terms are being finished. Pairing does not turn it on. New chats, files and saved keys stay on this computer; the relay refuses even encrypted uploads. Existing stored frames stay readable and nothing is deleted.

Pairing, seeing your devices and `--at` still work while the other computer is on. With storage off, new chat turns cannot be continued on another computer while the first is off, recovered from the relay after losing a computer, or sent as task copies. Teams sharing, synced keys, preferences and shared memories also wait. The relay still keeps device and directory coordination metadata, including existing chat records and leases; storage off does not erase those records.

After pairing, open **Settings → Devices → sync storage**. When it says **off**, press **enter** (`enter request access`). This opens a pre-filled GitHub issue; the panel also shows the request code and link if no browser opens. The request code is your identity id (`id_` followed by 32 hex digits). Approval is tied to this identity id. It is safe to post publicly and grants nothing by itself.

Turning sync storage off stops new uploads and leaves earlier frames readable. A self-hosted `codeaf relay --store <directory>` keeps storage on and needs no request. Approved hosted accounts still have the limits on *Relay limits and refusals*.

## How sync storage permission is discovered — custom addresses, startup and status

The relay's answer decides, whatever its hostname. When it reports storage permission, Settings shows the sync storage row and `/status` shows the dim line `sync storage  off` while off. A relay that answers without storage permission, including the self-hosted Go relay and older hosted Workers, keeps storage on and has no row or line.

Before any answer, the three hosted addresses on *Hosted relay or your own relay* start closed; other addresses start open. A paired process probes an unanswered custom address once at startup. A successful answer is remembered for this account and relay, including when the field is absent. Sync off and unpaired homes make no requests.

Eligible serving processes check after their first home-state round; chat windows check at launch and after `/status` in chat, and every ten minutes while a relay that reports storage is off (or a hosted address is still unknown). Storage on or an answer without the field stops requests. Once approval is read, waiting chats and uploads start within a minute without a restart. Opening a chat never waits for a relay reading. A revoked or removed device stops approval polling.

Pairing asks for the storage answer once alongside its other finishing work, with a best-effort budget of about 1.5 seconds. A timely on or off answer is saved for other open windows before pairing finishes. A timeout, failed ask or absent field leaves the saved answer untouched and adds no sentence.

## Does the relay store my chats — what is on the relay's disk

The relay holds two things for each identity, kept apart from every other identity: a **directory** (one record for each chat, one for each of your computers) and a **store** of chat content. New content uploads need sync storage, which is off by default until approval (Settings → Devices → sync storage); directory records can remain while storage is off.

The content is in the store as **encrypted chunks**. Each chunk is sealed under a key that only your own computers have. The relay cannot open a chunk. It keeps the chunks you send, and your saved keys too: they travel as one more sealed chunk, like a chat.

Two things in the directory are sealed before they leave your computer: **the title of every chat** and **the name of every computer**. The relay stores them as sealed text and cannot read them.

There is no email or password sign-in on the relay. Your identity is an id that starts with `id_`, made from your key.

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

## Scoped clients, slots and queued calls

The Go relay supports a client device kind with its own key pair and a list of
allowed methods. Full devices register the client before handing it its signed
certificate. A client receives no cell key, dedup secret, identity private key or
vault. It cannot read the directory or the blob store, change leases, pair another
device or change its own permissions. Revocation stops that certificate across
relay restarts. This is a relay protocol capability; the chat pairing commands do
not yet create scoped clients. The hosted Worker supports the same protocol.

The HTTP contract is `docs/relay-clients.md` in the source tree. An empty method
list allows offline reads only; adding methods requires a new paired key.

## Client slots and offline snapshots

A full device can replace a named sealed slot for one scoped client of the same
identity. Only that client reads the slot. The relay adds the writer device and
write time; readers can show when a snapshot was last refreshed. Each client has
at most 64 slots, each at most 64 KiB, expiring after 24 hours. Revoking the client
deletes its slots. The relay sees ciphertext and slot names, so names must not
contain private content. Slots persist across relay restarts. The publishing box
must seal to the client's current key and respect chat lease ownership.

## Client queue, offline calls and replay protection

Scoped clients can append sealed calls for an allowed method. Each call carries
a signed sequence number, time and target box, or `any`. Numbers must increase
by one, including after acknowledgement or expiry. Each client can hold 128
unacknowledged calls, each at most 4 KiB; calls expire at 24 hours. The relay
rejects replays, gaps, stale calls and methods outside the pairing's allowlist.
A client with no allowed methods cannot queue calls.

Full devices list work and claim an entry before executing it. An `any` claim
has one winner and survives restart; another box cannot acknowledge it. The
winning box acknowledges to delete the entry. Queue operations never acquire a
chat lease. Revoking a client deletes its queue. An executor must verify the
retained original request proof, permissions, age and any required biometric
confirmation inside the encrypted call, use durable execution idempotency and
log executed calls. The relay cannot read the call or verify biometric evidence.
