# Running your own relay

The relay is the service your computers pair and sync through. It stores encrypted
chat content and coordinates which computer holds each chat. Chat titles, computer
names, messages, files and keys stay encrypted. Routing ids, ownership, sizes,
times, platform capabilities and coordination progress remain readable metadata.

## Run and connect

```sh
codeaf relay --listen :8787 --store /var/lib/codeaf
```

The separate `relay` binary accepts the same flags.

| Flag | Meaning |
|---|---|
| `--listen` | Listen address; default `:8787`. |
| `--store <dir>` | Persistent coordination and encrypted chunks. Without it, only the blind transport and pairing mailbox run; chat sync needs storage. |
| `--quiet` | Suppress arrival, departure and request logs. |
| `--status=false` | Disable `GET /status`. |
| `--trust-proxy` | use the first `X-Forwarded-For` address for registration, dial and pairing limits; absent or blank falls back to the socket peer. only behind a proxy that sets it. |

Set `CODEAF_SYNC_URL=https://relay.example.com` on each computer, or use
`codeaf pair --via https://relay.example.com`. Pair the computers with the same
identity and relay. `CODEAF_SYNC_URL=off` stops sync, including existing clients.

forwarding headers are ignored by default. behind Railway, a load balancer or nginx, pass `--trust-proxy` or every caller shares one allowance. enable it only behind a proxy that sets the header.

The relay supplies no TLS or list of allowed people. Use a TLS proxy and a private
network or a proxy that checks callers: any valid signature can create an identity.
Logs contain a short identity id, route pattern, status and byte counts, never a body.
The native relay serves at the root of its host; give it a host of its own, or have
your proxy preserve the client's signed request path.

## Three anchors, joined computers and leadership

Each identity has three local persistent voters, called anchors. A directory change
commits on a majority before success; an authority read needs a fresh majority
response. One unavailable member is tolerated. All three anchors on this installation
share its host and disk: losing that host stops service. This command does not
configure independent anchor hosts. Encrypted content storage and the relay transport
must also remain reachable; an election does not replace them.

An open paired laptop or desktop with sync on joins as a durable learner: it receives
coordination metadata without adding a vote. Pairing two computers does not require
either to stay awake. From a joined computer using this relay:

```sh
codeaf cluster status
codeaf cluster lead
```

Status shows this computer, the leader, voters and learners when known, after a fresh
majority read. Lead catches up this computer, replaces the third anchor's vote through
a joint membership change, finishes that change with the anchor still learning, then
transfers leadership. Success prints `computer <node> is coordinating` followed by its
term, revision and read barrier. A pending request is not success; retrying lead reuses
the saved signed request. Keep codeaf open: without an open terminal the member runs
only for that command. Leadership does not transfer chat ownership or fetch content.

On a clean terminal quit, that personal leader releases its idle chats and returns
coordination to an anchor before stopping. Returning coordination adds at most five
seconds (`syncsetup.LeaderQuitWithin`); unavailable anchors retain ordinary fallback.
When that personal leader sleeps, crashes, loses its connection or turns sync off, the two remaining anchors
can elect a leader and restore the third anchor. Healthy fallback targets about
65 seconds; delays and partitions can take longer. Returning computers remain learners
until explicitly asked to lead again. Losing the personal leader and either voting
anchor leaves no majority. The refusal is `this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here`.
Saved local chats remain. There is no phone app yet, so a phone cannot lead yet.

## Storage layout and capacity

Everything is under `--store`:

- `raft/<identity id>/{1,2,3}.db`: separate FULL-synchronous SQLite WAL coordination stores.
- `raft/<identity id>/provision.json`: immutable cluster id and private protocol keys.
- `raft/<identity id>/transport-generation`: durable transport generation.
- `directory/<identity id>.db`: frozen original source and migration ownership receipt.
- `blobs/<identity id>/`: encrypted chunks and their object pointers.

Keep associated WAL files and the rest of the store together. Retained directory content
has an 8 MiB default allowance per identity; a larger imported identity keeps its
measured allowance. Bounded retry headers and membership initialization have a
separate allowance, so a full identity can record refusals and finish upgrading.
Capacity refuses content growth instead of deleting existing records.
Snapshots compact redundant coordination history, never saved chats, branches, vaults
or encrypted chunks. Allow disk space for three copies, snapshots and SQLite overhead,
as well as the separate encrypted content store.

## Back up and recover

Stop the relay with SIGTERM or ctrl+c and wait for in-flight requests to finish, then
copy the whole store. A consistent filesystem snapshot is another option. Copying
individual live database files can produce a torn backup. Restart with the same
complete store; keep its provisioning keys private even though they cannot decrypt chats.
The relay has no key that could recover a lost personal identity.

Two actual surviving anchor stores can coordinate in the normal three-anchor topology;
fewer than two refuse. Preserve damaged stores for recovery. Never replace one with a
fresh import under its old node id or reopen the frozen source as a writer. A stale
backup is not an ordinary restart: do not start it beside a live cluster. Automatic
recovery from a stale backup into a new writable cluster is not offered.

## Upgrade and older builds

Back up, stop the relay, replace the binary and restart with the same store. There is
no migration command or feature switch. First use freezes the old identity directory,
imports its rows into three anchors, checks a fresh majority and publishes one owner.
An interrupted import resumes the same cluster, keys and capacity allowance; the source
is retained and a damaged import refuses requests instead of truncating records.

This upgrade is one-way. Once the source is frozen, an older relay started against
that store refuses to change it. Its directory requests return HTTP 500 `internal`;
the underlying storage error is `codeaf: directory upgraded; use a current relay build`.
Running an older relay after the upgrade is not supported. Restart a current relay
with the complete store to resume. Encrypted chunks stay in their existing store.

Older clients keep pairing, listing, saving, publishing and releasing. Blindly forcing
a live chat without its observed holder and ownership number is refused with
`another device continued this chat first`. Quit on the holder or update the other
computer. Released or expired chats can still be taken. Safe conditional takes keep
their existing behavior.

Both relays preserve the original 1 MiB HTTP body limit for legal older directory
requests, including large signed metadata and frame hints. Bounded body staging makes
the final change atomic, rechecks the complete original proof and current authority,
and recovers interrupted requests without exposing partial changes. Sealed names, key
wraps and all frame hints remain intact. Over 1 MiB is refused, and retained storage
capacity still applies. A pairing protocol newer than the relay can still say
`that relay is too old for pairing`.

## Workers and idle traffic

A delegated worker uses its own expiring certificate and sealed grants for selected
chats. It cannot approve pairing, rotate the identity, read its vault, enroll devices
or become leader. Availability alone starts no tools: an assignment needs a committed
conditional claim. Interrupted work keeps its records and is never automatically
replayed. Worker setup and execution are not terminal commands yet. A scoped API worker
does not receive the full coordination log; explicit learner consent exposes
identity-wide routing metadata, with content and names still encrypted. The production
client does not activate delegated learners yet.

Caught-up idle homes with no work or held chats send zero HTTP requests, peer or
forwarding frames, or client attempts, including under personal leadership and after
fallback. All voters first apply the same idle barrier. Fresh input wakes coordination
and requires a majority response. Compatibility presence viewers still ping; only a
viewer causes presence sweeps, and presence alone never renews chat ownership.

## Hosted relay

The hosted relay in `relay/hosted` uses the same Go coordination core with three
separately addressed Durable Object anchors per identity and encrypted chunks in R2.
Those anchors share their provider and relay path, so they do not survive a
provider-wide outage. Joined computers can lead through the same commands. First use
upgrades an existing identity with the same one-way rule; per-identity and shared
storage and request limits still apply. Hosting account operations are separate from
running the native relay described here.

codeaf's own deployment is called the fabric and is served at `https://codeaf.agentfield.ai/fabric`,
the address every computer syncs through when nobody names another. It stores encrypted chat
content and the readable coordination metadata described at the top of this page, sees the time
and size of each request and the network address of each computer, and keeps no request log. The
address has a path, and every client keeps it. `codeaf relay` (this page) always serves from the
root of its host; behind your own proxy, pass the path through unchanged.

A hosted relay can instead set `BASE_PATH` to any path prefix, including several
segments such as `/a/relay`. Use that complete address when pairing. Signatures cover
the prefix, and directory operations and computer enrollment keep working there
when a joined computer leads. Requests outside the configured prefix return
HTTP 404 `not_found`.

## Architecture and workspace boundaries

See [workspace storage and relay architecture](architecture/workspaces-storage-relay.md)
for the shipped transport and the implemented, opt-in private-task and multi-root
contract. Private task copies and Review/Apply require
`CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1` with an absolute `CODEAF_HOME` on Linux or
macOS when creating the chat. Saved policies travel with compatible builds;
ordinary chats keep automatic task landing. Windows cannot enable this policy.
Every receiving computer needs a build that understands the private-task format:
an older build can take the chat and drop its tasks. Seal cost grows with retained
private roots. Warm-transfer frame hints use the Raft relay and stay off by default.
