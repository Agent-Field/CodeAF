# Reading a chat that lives on another computer

**Status:** built 2026-11-15 (`chats.read`, engine side). Needs furrow's path
reads (Agent-Field/furrow `feat/path-range-read`) and a pin bump before it is
offered anywhere.

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
