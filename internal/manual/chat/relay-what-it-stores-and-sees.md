# What the relay stores and what it can see

## Request access to relay storage (it used to be called sync storage) — turn on relay storage, turn on sync storage, why don't my chats sync to my other computer

Hosted relay storage is off by default while its privacy terms are being finished. Pairing does not turn it on. Saved keys stay on this computer, and no chat or file content is uploaded; the relay refuses even encrypted uploads. A chat can still move by blind pass-through between your paired computers while both are running codeaf: see *Blind pass-through*. Existing stored frames stay readable and nothing is deleted.

Pairing, seeing your devices, `--at` and your phone still work (*Does storage off stop my phone*); the other computer must be on for `--at`. With storage off, new chat turns cannot be continued on another computer while the first is off (blind pass-through needs both computers on), recovered from the relay after losing a computer, or sent as task copies. Teams sharing, synced provider keys, preferences, shared memories and idle handoff also wait. The relay still keeps device and directory coordination metadata, including existing chat records and leases; storage off does not erase those records.

After pairing, open **Settings → Devices → relay storage** in the terminal and press **enter** (`enter request access`) while it says **off**. In the desktop app, Devices and Advanced → Devices show **Relay storage** and **Request access…**. This opens a pre-filled GitHub issue. GitHub may ask you to sign in before showing the request. The terminal shows a request-code panel only when the browser cannot open; its **Open request on GitHub** link keeps the complete destination behind a short label. The desktop offers **Copy request link** if opening fails, and **Copy request code** while storage is off.

The request code is your identity id (`id_` followed by 32 hex digits). Approval is tied to this identity id. It is safe to post publicly and grants nothing by itself. The desktop reads this status locally and never contacts the relay merely to show the row. When blind pass-through is available, Devices and the pairing receipt explain that a chat can move while both computers are running. Its request button is offered only for hosted storage; a custom relay's storage permission belongs to its operator.

Turning relay storage off stops new uploads and leaves earlier frames readable. A self-hosted `codeaf relay --store <directory>` keeps storage on and needs no request. Approved hosted accounts still have the limits on *Relay limits and refusals*.

## How relay storage permission is discovered — custom addresses, startup and status

The server's answer decides, whatever its hostname. When it reports storage permission, terminal Settings shows the relay storage row and `/status` shows the dim line `relay storage  off` while off. A relay that answers without storage permission, including the self-hosted Go relay and older hosted Workers, keeps storage on and has no terminal row or line. The desktop still shows its effective storage permission.

Before any answer, the three hosted addresses on *Hosted relay or your own relay* start closed; other addresses start open. A paired process probes an unanswered custom address once at startup. A successful answer is remembered for this account and relay, including when the field is absent. Sync off and unpaired homes make no requests.

Eligible serving processes check after their first home-state round; chat windows check at launch and after `/status` in chat, and every ten minutes while a relay that reports storage is off (or a hosted address is still unknown). Storage on or an answer without the field stops requests. Once approval is read, waiting chats and uploads start within a minute without a restart. Opening a chat never waits for that reading. A revoked or removed device stops approval polling.

Repeated `/status` requests for the same home, identity and relay share a 30-second refresh window. The first request on a new binding is never held behind a previous account's refresh.

Pairing asks for the storage answer alongside its other finishing work, with up to three attempts inside a best-effort budget of about 1.5 seconds. A timely answer is saved for other open windows before pairing finishes, including a successful answer without the storage field. A timeout or failed reading leaves the saved answer untouched and adds no sentence. A revoked or gone identity is not retried.

Background chat publication waits silently while storage is off. It does not add a contradictory “could not be shared yet; will try again” notice or discard the chat's pending copy. Publication resumes after approval.

## Does storage off stop my phone — what a phone still sends the service

No. With storage off the service refuses chat uploads and keeps the device list, and a paired phone keeps working. That is also what `--storage off` does on a server you run yourself. A phone starts a chat on a computer, sends to it, stops it, answers it and reads its replies. Those travel as sealed slots and queued requests between the phone and one computer, which the service passes along without being able to read them. They are not chat storage and need no approval.

A chat a phone starts while storage is off leaves one directory record: the chat's id, the computer that holds it, its lease and its wrapped key. It has no title, no folder name and no head, so it names nothing the service cannot hold, and nothing of the conversation is uploaded. The chat stays on that computer and moves to another only by blind pass-through, as in *Blind pass-through*. The *Your phone* page says the same from the phone's side.

## Does the relay keep my chat when I move it without relay storage — what blind pass-through leaves behind

While hosted relay storage is off and a chat moves by blind pass-through, the relay keeps no chat or file content: the encrypted bytes pass through while both of your computers are running codeaf and are never written to the service. Everything stays encrypted end to end.

The service does hold a small directory record for each such chat, the same kind of thing pairing already stores. *What the relay records for a chat in blind pass-through* lists every field. The title and the folder name are not among them, so the service holds no chat name and no folder name. WorkAF can read the chat’s title directly from its computer over the same end-to-end encrypted connection.

`CODEAF_PASSTHROUGH=off` restores the previous behaviour: while storage is off, chats stay on the computer that has them. Blind pass-through does nothing when storage is approved or with a self-hosted relay. *Blind pass-through* has the steps.

## What the relay records for a chat in blind pass-through — the fields, the title and folder, the lease, what is never sent

While blind pass-through is the road, the directory record for a chat holds only: its id; the kind of chat; the chat it was split from, if any; the id of its latest saved turn and when it was saved; the ids of the encrypted chunks that make it up, and their total size in bytes; and the hold on it: which computer has it, the hold's number and expiry, how many saved turns are not yet ready to take, how far getting them ready has got, and whether a turn is running. It also lists your own identity with an empty sealed key: the keys that open a chat never reach the service.

**The title and the folder name are never sent**, not even sealed: not when a chat starts, not when it is saved, not for the separate chat a take keeps when you had edits that were never saved, and not by the background pass over older chats, which sends nothing at all while storage is off. The names stay in the chat on your computers; taking the chat is how a name reaches the other one.

Once relay storage is approved the record carries the sealed title and folder name again, as it always has. A self-hosted one always did. Message text is never part of this record: see *Why can't I send a message to a chat on another computer*.

## Why can't I send a message to a chat on another computer — messages wait for relay storage

A message to a chat that another of your computers holds is left in your computer's own record on the relay until that computer takes it, sealed with your key. That is keeping your text on the service, so while hosted relay storage is off nothing is sent: the desktop does not offer the message box for such a *waiting* message, and a send that arrives anyway is refused with `Relay storage is off for this account, so a message can't wait for another computer. Move the chat here to write to it.` Move the chat here instead; blind pass-through moves it while both computers run codeaf. A chat running on an online computer that you opened live (remote control on) is not a waiting message: the words go straight to that computer's running chat, sealed end to end so nothing in between can read them, and nothing is kept on the service, so storage being off does not stop it. *Open a chat running on another computer live in the desktop* has it.

Messages already waiting when storage was switched off can still be taken off the service. A self-hosted relay and an approved account send messages as before. *Send a message to a chat working on another computer* has the details.

## Does the relay store my chats — what is on the relay's disk

The relay holds two things for each identity, kept apart from every other identity: a **directory** (one record for each chat, one for each of your computers) and a **store** of chat content. New content uploads need relay storage, which is off by default until approval (Settings → Devices → relay storage); directory records can remain while storage is off.

The content is in the store as **encrypted chunks**. Each chunk is sealed under a key that only your own computers have. The relay cannot open a chunk. It keeps the chunks you send, and your saved keys too: they travel as one more sealed chunk, like a chat.

Two things in the directory are sealed before they leave your computer: **the title of every chat** and **the name of every computer**. The relay stores them as sealed text and cannot read them. While blind pass-through is the road, the title of a chat (and its folder name) is not sent at all: see *What the relay records for a chat in blind pass-through*.

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

## What reaches the relay while a view is direct — setup messages, then nothing, does a STUN server learn anything about my chats

A live view from another of your computers (*Remote control*) can move onto a direct connection between the two. Finding each other takes a few small messages sealed with your key that pass through the relay. Once the view has moved, none of its traffic goes there: typing, replies and files go straight between the two computers, so they are not in the byte counts or the daily allowance that the server keeps.

A STUN server is a separate thing (by default Cloudflare's, then Google's). It is asked for this computer's public address and learns only that an app at that address asked; it never sees a chat, a name or a key. `CODEAF_REMOTE_ROAD=relay` asks none and never offers a direct connection. `CODEAF_REMOTE_STUN=off` asks none and still lets two computers on one network connect directly.

## Does the relay see my code, my prompts or my files

Hosted storage is off until approved; no new chat or file chunks are kept while off. With storage on, turns, tool output and files are sealed on your computer before they are sent. The relay gets ciphertext and sizes.

Your saved keys are sealed too, as said above.

Continuing a chat on another computer brings its saved workspace files with its conversation, tool output and task journals. Rebuildable install folders and withheld files are listed on the arrival card. Secret files travel separately between paired devices: in the sealed vault, or, while hosted storage is off and the chat moves by blind pass-through, sealed with the chat and stored nowhere. See *Continuing a chat that one of your machines holds* and "What comes with a continued chat" on *Home*. It does not keep two arbitrary project folders mirrored while you edit them.

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
at most 128 slots, each at most 64 KiB, expiring after 24 hours. Revoking the client
deletes its slots. The relay sees ciphertext and slot names, so names must not
contain private content. Slots persist across relay restarts. The publishing box
must seal to the client's current key and respect chat lease ownership.

## Enrolled phones, the computers list and live slot waits — which of my computers are serving my phone, how a phone is told to refresh

A phone is enrolled once, with a statement signed by the identity key naming the
phone, its two public keys, the methods it may call and the names it may read. The
server stores that statement beside the phone's other data. At most 16 phones can
be enrolled per identity, and a statement can be replaced only by one with a
higher version. The server reads none of it; it checks only the shape and the
identity signature, and every computer checks the statement again itself.

A phone can ask the server for the identity's computers. The list names each
non-revoked computer, whether it is serving phones right now and when it was last
seen. A computer counts as serving only while it holds a directory watch open
with `serving=1`, so a computer that is switched off or asleep stops showing as serving
shortly after its socket goes quiet.

A phone can also wait for its slots to change: a read that names the last version
it saw and a wait of up to 25 seconds returns as soon as any slot changes, or with
nothing when the time is up. A phone may hold two waits, and revoking the phone
ends them at once. While a phone is waiting or reading, the server tells serving
computers that it is watching, and a write to a slot answers with a
`Codeaf-Client-Watching: 1` header, so computers publish at once only for a phone
that is looking. The server never reads slot contents.

## Client queue, offline calls and replay protection

Scoped clients can append sealed calls for an allowed method. Each call carries
a signed sequence number, time and target box, or `any`. Numbers must increase
by one, including after acknowledgement or expiry. Each client can hold 128
unacknowledged calls, each at most 4 KiB; calls expire at 24 hours. The server
rejects replays, gaps, stale calls and methods outside the pairing's allowlist.
A client with no allowed methods cannot queue calls.

Full devices list work and claim an entry before executing it. An `any` claim
has one winner and survives restart; another box cannot acknowledge it. The
winning box acknowledges to delete the entry. Queue operations never acquire a
chat lease. Revoking a client deletes its queue. An executor must verify the
retained original request proof, permissions, age and any required biometric
confirmation inside the encrypted call, use durable execution idempotency and
log executed calls. The server cannot read the call or verify biometric evidence.
