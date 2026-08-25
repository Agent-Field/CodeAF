# Remote files over ssh — what the wire costs

Measured 2026-08-24, `spark` → `blackmac`, over a Tailscale tailnet link over a direct WireGuard path between two machines on the public internet — not a local network.

- **surface** — this machine, linux/arm64, running the local half in-process: `internal/remote`'s client, dialled exactly as `aforge chat --host` dials it.
- **engine** — `blackmac`, Darwin arm64, running `aforge engine --no-host --workspace 'af-files-e2e'` under `ssh -T`, which is the shape that serves the pipe and ends with it. Workspace as the engine resolved it: `/Users/santoshkumar/af-files-e2e`.
- **wire** — v3 on both ends. The handshake refuses a mismatch at the door, so every number below is one protocol talking to itself.

Reproduce, with a seeded workspace on the far machine (`note.txt`, `sub/inner.txt`, `five-mb.bin`, `twenty-mb.bin`) and an aforge from this branch on its PATH:

```
AFORGE_BENCH_HOST=blackmac go test -tags ssh_bench -run TestRemoteBench -v ./internal/e2e/
```

Every figure is the **median of the whole run** — no best-of, no discarded repetitions — and every fetch's sha256 was checked against the seeded file before its time was counted.

| measurement | n | median | p95 | note |
| --- | --- | --- | --- | --- |
| round trip, smallest useful call (`StatPaths` on one path) | 50 | 7.2 ms | 9.9 ms | the floor under every other number here |
| one fresh ssh connection (`ssh <host> true`) | 5 | 313.2 ms | 334.5 ms | what every `scp` pays and an established wire does not |
| cold dial — ssh, `aforge engine` starting over there, handshake | 3 | 424.0 ms | 446.9 ms | paid once when a session opens, and again by every redial |
| `ListDir` of the seeded workspace (4 rows) | 20 | 7.6 ms | 9.5 ms | a listing this small is a round trip and nothing else |
| `ListDir` of a 1000-entry directory | 10 | 27.5 ms | 109.1 ms | under the engine's 2000-row ceiling, so nothing is cut |
| `FetchFile` of five-mb.bin (5242880 bytes) | 10 | 526.6 ms | 705.1 ms | 9.5 MB/s over the wire, sha256 checked every time |
| `scp` of the same file to a local temp file | 10 | 655.9 ms | 930.41 s | 7.6 MB/s; includes a local disk write the wire's fetch does not do |
| the wire against raw `scp` |  |  |  | the wire is 20% FASTER: `scp` opens a fresh ssh per copy, the engine is already connected |
| second open of a file already held — stat, then hash what is here | 5 | 10.2 ms | 98.5 ms | 0 of the file's 5242880 bytes cross |
| second open as a full fetch instead | 5 | 447.7 ms | 458.3 ms | 44x the work of the line above, for bytes already on this disk |
| the gap — ssh child killed, until a call is taken again | 1 | 1.51 s |  | 15 calls refused in it with `reconnecting to blackmac — try that again in a moment` |
| kill to the next VERIFIED 5MB fetch | 1 | 1.96 s |  | the gap above plus the fetch itself, digest checked |
| refusal of twenty-mb.bin (over the 16MB ceiling) | 5 | 7.7 ms | 9.2 ms | the size is read off the far disk, so the no comes back at round-trip speed |

## What the rows mean

**The round trip is the floor.** Every call is one frame out and one frame back, so nothing here is faster than a stat: 7.2 ms. A listing of a small directory costs the same, because it IS the same round trip; a thousand entries costs the round trip plus the JSON.

**The wire is not the slow way to move a file.** Fetching the 5MB file over the wire — frames, base64, one JSON round trip — came back **20% faster** than `scp` of the same file on the same link (526.6 ms against 655.9 ms). That is not the encoding being free; it is one whole ssh connection being expensive. `scp` opens one per copy, and the wire's engine is already connected. At a large enough file the encoding would dominate again and the ordering would swap; what this run says is that at the sizes a person actually clicks on, the encoding is not the term that matters. What the wire buys, which `scp` has no way to offer: the transfer rides the conversation's OWN connection, so it redials itself when the link drops; the path was authorized by the engine under the two-roots law rather than by whatever that account happens to be able to read; and nothing asked the person for a second credential — no second authentication, no second host key, no second window.

**The dedup path is the real speed-up, and it is a hash comparison.** A file already held is not fetched again: the surface asks whether the path is still there and compares the digest it already has (`FetchedFile.Hash`, the CAS's own key). None of the file's 5242880 bytes cross — the difference between those two rows is the difference between a question and a transfer.

**The transfer survives the cut.** The measurement is literal: two 5MB fetches complete, this harness kills its own ssh child by its process handle, and the clock runs until a fetch succeeds again AND verifies. In the gap calls are refused rather than queued — `reconnecting to blackmac — try that again in a moment` — and internal/remote's reader goroutine opens the next ssh itself. The gap is not mysterious once the cold-dial row is beside it: one second of first backoff (redial.go's `firstBackoff`), then a whole cold dial (424.0 ms here — ssh, plus `aforge engine` starting over there), and the fetch that follows takes what a fetch takes. A persistent engine host, which is the door's default shape, replaces the boot with a socket attach and is the faster of the two.

**The refusal is fast.** A file over the 16MB ceiling is refused from its SIZE on the far disk, before a byte is read, so the no comes back at round-trip speed rather than after 20MB of transfer: `engine: twenty-mb.bin is 20MB and the most one file may cross this connection is 16MB`.

## Honest asymmetries in these numbers

- `scp` writes to a local file and the wire's fetch holds bytes in memory, so the `scp` row carries a disk write the fetch does not. If anything that is kind to `scp`.
- Each `scp` includes its own connection setup (313.2 ms median for a bare `ssh <host> true`); the wire's fetches are timed on an engine that is already up, and the connection they amortise is the cold dial row. Both are what the two tools actually cost a person, which is why they are compared as they are rather than adjusted.
- The far engine is run with `--no-host` so that nothing but ssh, frames and an engine is in the middle, and so that no session host is left running on somebody else's laptop. The door's default attaches to one.
- The thousand-entry directory is made and removed by the run itself, inside the workspace; the engine's own session journal is written where that machine keeps journals, as it is for any `--host` session.
- macOS answering a Linux terminal often prints a locale warning on ssh's stderr. It is noise and changes nothing measured here.

## What stalled during this run

- One repetition of **`scp` of the same file to a local temp file** took 930.41 s against a median of 655.9 ms. That is a link that stalled, not a cost of the thing being measured; it is left in the table because a benchmark that removes its worst repetition is a benchmark that has stopped measuring the link it ran on. Read the median.
