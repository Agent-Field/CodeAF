# Remote files: see what it made, browse what it has, move things — the plan

The wave after remote access (PR #45). One goal, one law, three lanes.

**The goal.** On a `--host` (and later `--at`) session: a path the chat names can
be OPENED — an image the model produced, a log, an asset — with the local
machine's own viewer; a browse page shows the far workspace; files move between
the machines through aforge. Read-only on the far disk: no editor loop, no
write-back, no arbitrary remote writes. Viewing is the product.

**The law that shapes everything: the two roots.** `handOver` (internal/remote/
file.go) already decides what may cross: the workspace and the session's own
folder, symlinks resolved, regular files only. ListDir and StatPaths answer
under the SAME law. The browse view is a view of the conversation's places, not
of a machine.

**Transport.** Everything is wire frames (v3). ssh today; the relay road gets
all of it unchanged the day a relay is deployed, because frames are frames.

## Decisions (made, not open)

1. **No editor, no write-back.** Click-to-VIEW. The mirror copy is bytes to
   look at; editing it changes nothing over there, and the manual says so.
2. **Uploads land in `attachments/`**, never an arbitrary remote path. Moving a
   file into the workspace proper is a sentence to the model, which already
   owns consent for writes.
3. **Prefetch, not push.** No new push machinery: when the surface sees a tool
   event that WROTE a file (write/image hints), it speculatively FetchFiles it
   into the CAS, ≤ 2MB per file (`prefetchMax`), so the click is a cache hit.
   The wire did not change for this; only the surface got eager.
4. **Cache by content.** FetchedFile now carries sha256 (the CAS's own digest).
   Surface checks `cas.Stat(ref)` before fetching twice. Mirror path for the
   opener: `~/.aforge/v3/remote/mirror/<host>/<abs path>` hardlinked (fallback
   copied) from the CAS blob, so a viewer's title bar names the real file.
5. **Links are honest or absent.** Hosted pathlink stays OFF until StatPaths
   confirms a word (batched, debounced, ≤64 per call). Confirmed paths link to
   the door's `/f/<id>` — cmd+click parity with local sessions — and press
   in-app opens the same way.
6. **The door is capabilities.** 127.0.0.1, OS-chosen port, per-door token,
   minted ids. The door adds no law of its own; refusals pass through verbatim.
7. **Ceilings honest.** >16MB does not cross (existing sentence). Browse page
   states it on such rows rather than spinning.
8. **Emptiness law.** A fetch under 300ms says nothing. Longer draws one quiet
   note; failure is the engine's sentence once.

## Lanes and ownership (parallel, one worktree, disjoint files)

- **Lane A — engine + loopback proof.** OWNS: `internal/remote/file.go`
  (extend), `internal/remote/browse_test.go`, server dispatch in `server.go`.
  ListDir + StatPaths handlers beside handOver under its law; `listDirMax`
  (2000) + Truncated; `statPathsMax` (64) refusal; fill FetchedFile.Size/Hash
  in fetchFile. Loopback tests for all three; `-race`.
- **Lane B — the door.** OWNS: `internal/filedoor/*`. Real Open/FileURL/
  BrowseURL/Close per the frozen API; embedded single-page browse UI (no
  external assets, dark/light honest); GET /f/<id> with MIME; /api/ls,
  /api/put (Deposit); drag-drop upload; download = the browser's own. Tests
  over a map-backed Source, httptest.
- **Lane C — the surface.** OWNS: `internal/tui3/remotefiles.go` (+_test),
  `remoteopen.go`, edits to `pathlink.go` hosted gate, `commands.go` (/files),
  `app.go` wiring, `cmd/aforge/chatv3_host.go` (hand the client to the app as
  the door's Source). Narrow interfaces by shape (attach.go's pattern), so
  local sessions and tests fake them. Prefetch-on-write; open flow
  (CAS → mirror → opener.go's platform hand-off); /files opens BrowseURL.
- **Lane D — manual + docs (after A–C land).** New page
  `opening-files-from-that-machine.md`; update running-on-another-machine.md,
  attaching-files.md, commands.md (+ /files in every gate); this file becomes
  the design record; remote-access-testing.md gains a §3 entry.

## Deferred, on purpose

Thumbnails computed engine-side; range/tail fetch past 16MB; content-defined
chunking + Merkle diff for big trees (wave 2, when someone actually moves
them); `--at` e2e (blocked on a deployed relay, same as PR #45).

## What landed

The wave is in, on `remote-files/v0`, in four commits. This file is the design record from
here on; the manual is the account a person reads (`opening-files-from-that-machine.md`),
and `docs/remote-access-testing.md` §3f is the hands-on recipe.

| Commit | Lane | What it landed |
| --- | --- | --- |
| `d24a153b` | A — engine | `List.Dir` and `Stat.Paths` beside `handOver` under the same two-roots law; `listDirMax` 2000 with `Truncated`; `statPathsMax` 64 refused rather than trimmed; `FetchedFile` gained `Size` and `Hash` (sha256 of the bytes that actually crossed). Loopback proofs in `internal/remote/browse_test.go`. |
| `ef54c75b` | B — the door | `internal/filedoor`: a `127.0.0.1` listener with a per-door token and minted per-path ids, `GET /f/<id>` with MIME and range, `/api/<token>/ls`, `/file`, `/put`, and the embedded browse page — dirs first, sizes and times, click-through, drag-drop upload, a row over the ceiling drawn plain with `too big to cross`. A wrong token and an unknown id answer the same 404. |
| `899e42b7` | C — the surface | `internal/tui3/remotefiles.go` and `remoteopen.go`: the fact table and its batched, debounced `StatPaths`; the door opened on first need and closed with the surface; prefetch-on-write at `prefetchMax` (2MB); the CAS at `~/.aforge/v3/remote/cas` with the digest verified here rather than trusted; the hardlinked mirror at `~/.aforge/v3/remote/mirror/<host>/<engine path>`; `/files` and `/files <path>`; `pathlink.go`'s far-side branch. |
| `3939852a` | F — the deposit | `Deposit.File` on the wire: bytes land in the far session's `attachments/` folder, under the same name law and naming as `/attach`, and **nothing else happens**. |

**One decision was reversed on the way.** Lane C shipped the browse page's drag-drop lane
refusing, because the only wire door that wrote into a session's attachments was
`Submit.Files` — a MESSAGE, which would have opened a model turn nobody at this end asked
for and streamed its answer into a channel nothing was reading. Decision 2 above said
uploads land in `attachments/`, and lane F made that true properly by giving the wire a
door that keeps a file without submitting it. The proof that it stays silent is
`internal/remote/file_test.go`'s `TestADepositedFileLandsInAttachmentsAndOpensNoTurn`: the
bytes are on disk in the right folder, and no turn, no event and no transcript entry
followed them.

Everything under "Deferred, on purpose" is still deferred, and `--at` is still blocked on a
deployed relay.
