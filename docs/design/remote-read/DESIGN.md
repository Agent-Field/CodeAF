# Reading a chat that lives on another computer

**Status (2026-11-24): built, and in the tree beside dev's reader.** The
`chats.read` method described below was built on 2026-11-15 (#2650). When the
organization server's branch landed on `dev` it was dropped as a duplicate of what
`dev` already had for the same ask, which is a different thing: `agents.entries`
on a chat held elsewhere reads its saved head into private scratch and pages it
with `before` and `limit`, and the hello word `chats.read` says that works. That
reader fetches the whole saved transcript and keeps nothing a later take could
reuse. Nothing Santosh built is dropped, so the chunk-paged reader is back,
additively: the `chats.read` method is advertised under its own hello word,
`chats.read.paged`, and `chats.read` keeps the meaning it has on `dev`, so a
client can tell which of the two abilities an engine has. `agents.entries` and
its tests are unchanged. The embedded furrow is v0.3.7 again, because the method
needs the path-read verbs that v0.3.6 does not have; with an older furrow the word
is absent from hello, not present and failing.

## The ask

A chat another paired computer holds opens in the desktop as a real, read-only
chat: newest messages first, older ones as the person scrolls. It must NOT take
the chat over, and it must NOT download the chat's project folder. Reading what
was said has to cost about what the words weigh.

## Why a take is the wrong tool

A chat is a cell: a folder whose `.cell/transcript.jsonl` is its transcript, and
every finished turn is sealed into one furrow snapshot that also holds the
project folder. Until now the only reader of a remote snapshot was a take, and a
take's `want` walks the WHOLE snapshot. For a real project that is hundreds of
megabytes or gigabytes before one line can be shown — and a take also leases the
chat away from the computer that was using it.

## The design

Fetch only the objects on the path to the transcript, and only the chunks under
the bytes being shown.

| step | what is fetched | about |
|---|---|---|
| open | snapshot, trees down to `.cell/`, the transcript's index object | a few KB |
| | the chunks under the newest page | 256 KiB–2 MiB |
| | the transcript's first chunk, once, for its header line | 64 KiB–1 MiB, never again |
| more | the chunks before the cursor | one chunk or so a page |
| new turn | the new head's index objects and the new tail chunk | ~one chunk |
| take afterwards | everything the read did not bring | never a byte twice |

furrow chunks files by content (64 KiB min / 256 KiB avg / 1 MiB max), so
appending a turn leaves every earlier chunk byte-identical. A growing
transcript therefore re-downloads only its tail.

### Measured (real furrow, `internal/cellsync/read_real_test.go`)

A chat with a 12 MiB project file and a 4 MiB transcript (16.8 MB in all):

- first page (20 entries): **644 KB fetched, 3.8%** — the first chunk for the
  header and the tail chunk;
- after one more turn: **294 KB** — the new tail chunk and the index objects;
- a take afterwards fetched 69 objects, **none** of the 11 the reads brought.

The same numbers come out over spawned engines and over the managed daemon.

## Where each part lives

- `internal/cellstore/pathread.go` — the engine verbs: `want --path [--range]`,
  `peek`, `read`, and the read-side import, all against the chat's own store and
  run from codeaf's store root, because the chat's folder does not exist on a
  computer that only reads it. `ReadsPaths` asks `furrow --json capabilities`.
- `internal/cellsync/read.go` — `Reader` (want → gather → import, scoped to one
  path, counting bytes) and `Pager` (chunk-aware pages of the transcript).
- `internal/syncsetup/read.go` — `Sync.ReadChat`: the take's engine, keys,
  ledger and per-chat Scope, but no lease and no restore.
- `cmd/codeaf/app_chatread.go` — `chats.read`, shaped by the same
  `appEntryPageOf` that `agents.entries` uses.

## Paging

- A page is cut only at the start of a line that is a person's message, so a
  tool call is never separated from its result and a page never opens mid-turn.
- The pager walks back one chunk at a time from the cursor and binary-searches
  the latest cut that still shows at least `limit` entries. A turn longer than
  `limit` is one page.
- `before` is opaque: the head the paging began at and the byte offset the next
  page ends at. Paging back stays on that one snapshot while new turns land.
- The header line (`{"type":"session",...}`) is put in front of every page cut
  from the middle, so a transcript a newer codeaf wrote still says so.
- Entry ids get the page's byte start as a prefix (`r<start>.`): ids count the
  person's messages from the top of what was shaped, which is the page.

## Laws

- **Same store as a take.** A read imports into the chat's store with the take's
  own import verb, so `Continue here` after reading downloads only the rest.
  The read keeps its own inbox (`<inbox>.read`): a strict import refuses files
  no pass of its own wanted, and a take or watching daemon may be filling the
  shared one.
- **No lease, no materialize, no agent, no folder.** The holder keeps the chat;
  this computer keeps objects in its store and nothing else.
- **Absent when it cannot work.** `chats.read` is in `hello` only with a relay
  configured and a furrow whose capabilities list `read`; it is asked, never
  tried and failed.
- The relay, its encryption and its convergent ids are untouched.

## Measured on two machines (2026-11-16)

Two GCP e2-standard-4 machines, the holder in Toronto (with a `codeaf relay` beside it) and the
viewer in Iowa, driving `codeaf app` over its wire. One chat of 12 turns working in a 601 MB folder.

| Journey | Time | Bytes |
| --- | --- | --- |
| Read here, newest page | 411 ms | whole transcript 116.6 KB; older pages 0 B, ~100 ms each |
| Same head read again | 107 ms | 0 B |
| Send from the viewer, reply visible there | 7.5 s with the model | one tail chunk |
| Follow the holder's turns | 3.8-6.1 s after each send | one tail chunk |
| Continue here (take) | 41 s | whole folder |
| Take back to the first computer | 19 s | 559 KiB, the changes only |
| Holder offline: read / Continue here | 145 ms / 19 s | 0 B (held) / folder |
| Cold take with nothing read | 38.5 s | 606.2 MiB |
| Read every page first | 517 ms first page | 190 KiB on the wire |

The viewer's whole state after reading was 9.4 MB, with no project file in it. Reading the full
transcript cost about 0.03% of a take, which is the point of this design: do not trade it for a
take "to keep it simple".

## Do not simplify this into a full fetch or a materialize

It is tempting to "reuse" `Fetcher.Complete` or a take and read the file off
disk. That is exactly the cost this exists to avoid: it downloads the project
folder, and a take displaces the computer using the chat. If a future change
needs more of the cell, add a path to read, not a walk of the head.

## Not covered

- The turn running on the holder right now is not on the relay; a page shows
  what was last sealed (`sealed_at_ms` says when).
- Turn footers (receipts), kept-process notices and tool output facts are local
  records on the holder and are not on a remote page.
- Entry ids are stable within a head's pages; a client replaces its pages when
  `head` changes.

## Crews on another computer

**Status:** built 2026-11-16 (`chats.crew`, engine side; hosted relay needs the
`Codeaf-Chat-Crews` deploy).

A crew made on one computer has to show on a paired computer as ONE crew — its
manager and members grouped, not loose chats — even while the holder is off,
and stays read-only there: the only thing done to it from elsewhere is
`chats.send` to its manager.

**The crew rides the chat's directory record**, sealed under the identity's
metadata key with its own associated data (`directory.SealCrew`), beside the
sealed title and folder. Each publish seals `{v, id, name, manager, kind}` for
the crew `teams.ChatCrew` picks, or `{v:1}` for "in no crew" (an empty field
keeps the old value, so leaving a crew has to be said). The viewer opens it in
`chatlist.WithCrews` and `cmd/codeaf`'s `appChatCrew` puts it on the row; this
computer's own teams store wins for any chat it lists.

Why the record and not the vault's team sync (`syncsetup/teams.go`):

- The vault sync MERGES the crew into the viewer's teams store as a crew the
  viewer can edit and drive. Two computers driving one crew is the thing this
  version must not allow; the record only describes.
- `codeaf app` runs the vault round only while pairing is still pending, so a
  crew made after pairing never travelled — the live GCP pair on 2026-11-16 got
  `crews.list: []` and three loose member rows for exactly this reason.
- The record is already per chat, durable on the relay while the holder is off,
  and rewritten every saved turn.

Costs and limits, accepted on purpose:

- A rename, new manager or removal on the holder arrives with that chat's next
  saved turn; an idle chat keeps what it last said.
- A relay that does not answer `Codeaf-Chat-Crews: 1` never receives the field
  (older relays decode strictly), and its rows are loose. The header is not in
  `directory.Capabilities`, so it does not block a release.
- Crews do not appear in `crews.list` on the viewer, and taking a crew whole is
  not offered. Do not "fix" that by turning the vault team sync on in the app
  without first deciding who drives a crew whose members live on two computers.
