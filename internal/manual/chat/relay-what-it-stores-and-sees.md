# What the relay stores and what it can see

## What does the relay store — what is on the relay's disk

The relay holds two things for each identity, kept apart from every other identity: a **directory** (one record for each chat, one for each of your computers) and a **store** of chat content.

The content is in the store as **encrypted chunks**. Each chunk is sealed under a key that only your own computers have. The relay cannot open a chunk. It keeps the chunks you send, and your saved keys too: they travel as one more sealed chunk, like a chat.

Two things in the directory are sealed before they leave your computer: **the title of every chat** and **the name of every computer**. The relay stores them as sealed text and cannot read them.

There is no account on the relay. No email, no password, no name. Your identity is an id that starts with `id_`, made from your key.

## What can the relay see — the metadata, my IP address, and what is not hidden from it

**The relay cannot read:** your messages, your files, the titles of your chats, the names of your computers, your saved keys, the pairing code, the three words.

**The relay can see, and keeps or logs:**

- your identity id, and the id of each computer you use (an id, never a name);
- for each chat: its id, how big it is, when it last saved, which computer holds it, and the chat it was split from;
- for each computer: its operating system, processor type, and whether it has a sandbox or a copy-on-write disk;
- for each chunk: its size, and the ids of the parts inside it;
- when you connect, how often, and how much you send;
- the network address each request came from.

A relay you run yourself logs only the first 8 characters of the identity id, the kind of request, the status and two byte counts. It never logs a body. An operator of a relay you do not run can do more than that; you cannot check it.

The relay can also delete what it holds, or refuse to serve it. Keep a copy you care about on a computer of your own.

## Does the relay see my code, my prompts or my files

No. Turns, tool output and files are sealed on your computer before they are sent. The relay gets ciphertext and sizes.

Your saved keys are sealed too, as said above.

Continuing a chat on another computer brings its saved workspace files with its conversation, tool output and task journals. Rebuildable install folders and withheld files are listed on the arrival card. Secret files travel separately in the sealed vault between paired devices. See *Continuing a chat that one of your machines holds* and "What comes with a continued chat" on *Home*. It does not keep two arbitrary project folders mirrored while you edit them.

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

## Paired client snapshots from the app backend

The app backend keeps snapshots fresh for locally paired scoped clients. It uses
the desktop app methods `agents.list`, `asks.list`, `tasks.list`, `inbox.list`,
and `spend.today`; chat results cover only chats whose live lease this computer
holds. Each client decrypts its own snapshots with its independent encryption
key. Snapshots say which computer wrote them and when. Empty chat lists retain
their chat identity so they can replace older results.

Publishing a turn and app changes schedule a refresh. An active client also gets
a five-second refresh timer, backing off failures to five minutes. Unchanged
results are skipped until their twelve-hour renewal. With no active client,
there are no extra snapshot reads, relay requests or refresh timers. Encryption
keys and read subscriptions are enrolled locally on each executing computer;
they are not yet distributed by the directory.

## Queued answers, Face ID and interrupted delivery

The app backend executes locally paired clients' queued `asks.answer`,
`agents.send`, `agents.stop` and `agents.create` calls through the desktop
methods. Other queue methods are refused at pairing. Chat calls run only where
this computer already holds the chat's live lease; they never take it from
another computer. Starting new work targets `any` and has one claim winner.

An irreversible ask requires the queued call's signed `face_id` flag. This is
the client's assertion that it obtained biometric confirmation. The computer
checks the ask again rather than trusting the snapshot the client saw. It also
checks the retained signature, identity, sequence, age and method permission.

Every executed call leaves who, when and method in its chat. A receipt is kept
before execution. If delivery is interrupted at that point, codeaf does not
repeat the possible effect automatically. An answer that finished but lost its
acknowledgement is acknowledged after restart without being run again. Gaps in the local sequence are refused unless the client explicitly signs a
`skip_through` instruction for the missing numbers. That skipped local range
is kept in the chat receipt. A refused earlier pending entry still stops the
round. Started or completed calls are never replayed by a skip instruction.

The same client can enroll locally on several computers with the same allowed
methods. Each computer has its own encryption key. An `any` call may contain a
sealed copy for each eligible computer; the relay's atomic claim still chooses
only one to execute it.

## List paired clients, pause a client, or unpair one

The app backend provides `clients.list`, `clients.pair`, `clients.pause` and
`clients.unpair` over appwire and advertises `clients.manage`. These are backend
Settings controls; no new terminal slash command or desktop widget is added.
Pairing requires approved signing and encryption public keys, requested snapshot
reads and queued-call permissions. The returned grant contains public connection
information only. The ordinary chat pairing commands still pair full devices.

Pause stops this computer's snapshot refresh and queue execution and survives
restart. It waits for a current round to finish. It does not erase slots the
client already has. Resume uses `clients.pause` with `paused:false`. Unpair
revokes the certificate at the relay, which deletes its slots and queue. Kept
call receipts stay in the chat. These management mutations require a trusted
live appwire connection; the v0 scoped queue does not grant them.

## Chat identity in app snapshots and queued answers

A cell-backed chat keeps its transcript inside `.cell/`, but its app identity is
the enclosing conversation folder's ID. That same ID scopes snapshots and
queued answers and names its relay lease. The app resolves both cell and older
transcript layouts through the shared session layout reader. Closing the app
cancels and joins its client runner before closing the conversations it uses.

## Snapshot too large or one subscribed read fails

Each encrypted method snapshot must fit the relay's 64 KiB slot limit. If it
cannot fit, codeaf refuses that update and keeps the old snapshot and timestamp.
It continues updating other subscribed methods; a failed read does not stop
all the client's snapshots. v0 does not paginate or silently truncate large
snapshots. After the last client is unpaired, startup retains old call receipts
without opening sync or starting a client refresh loop.
