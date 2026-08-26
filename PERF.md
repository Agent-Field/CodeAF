# The performance laws

A wave of work in August 2026 took a millisecond and half a megabyte off every
provider call, stopped the frame redoing what nothing asked it to redo, and took
two thirds off the embedded corpora. This file is what keeps it. Every win below
is defended by something that goes red locally, in `go test` or in `make check`,
with a message that says what happened.

## The doctrine: gate on work, never on time

**No gate in this repository is allowed a wall-clock threshold.** Every one of
them counts something exact — allocations, blocking calls, decompressions,
bytes — because a count is a fact about the code and the same fact on a loaded
laptop, on idle CI and on a machine three years faster. A stopwatch is a fact
about the weather. A suite whose red means "the box was busy" is a suite people
learn to re-run instead of read, and one flaky perf test costs more trust than
the regression it was guarding against.

**Any change to a number below must change this file in the same commit.** The
caps are allowed to move — a feature is sometimes worth its bytes, a fix
sometimes pays a residual off — but moving one is a decision somebody signs for
in a diff, never a drift nobody saw.

## The size ratchet

`SIZE-BUDGET` holds one number: the bytes the stripped binary may weigh. `make
check` builds and weighs; over budget is a hard failure naming the size, the
budget, and the overage. `make build` alone never gates, because a developer
rebuilding twenty times an hour must not be stopped by a byte count.

Over budget, there are exactly two honest moves: shrink what you added — pack a
corpus (`internal/packed`), drop a dependency, stop embedding what can be
fetched — or raise `SIZE-BUDGET` in the same commit as the thing that spent it.

The budget was set on linux/arm64 at 48,890,121 bytes with about two percent of
headroom. A different platform or a new Go release moves the figure on its own;
that too is a reason to reset it deliberately, never to ignore red.

It was reset once since, on linux/arm64, when the perf wave merged into
`chat-v3-task`: the remote-access wave that landed on that branch in the meantime
— the relay, the pairing door, the engine host, the far-disk file surface, the
media notes — weighs 201,769 bytes more than the budget the perf wave was
weighed against, and none of it is embedded data a packer could take back. The
binary measured 50,069,769 and the budget was set to 51,071,000, keeping the
same two percent of headroom the first figure was given.

## The flush ceiling

`FlushUsage` waits at most **2 seconds** (`usageFlushLimit`, `internal/session/usage_ledger.go`)
for every ledger it is flushing, together rather than each. It is the exit path's
cap, not a turn's: `v3Process.closeAll` calls it so the last turn's spending is on
disk before the terminal comes back, and a writer parked inside `openUsageLedger`
on a stalled mount never drains its queue again. Unbounded, that flush never
returned and the terminal never came back — the one failure the rest of that file
is written to prevent, moved off the turn path and onto the exit. The bargain is
the file's own: a spending record is worth less than the turn that earned it, and
less than the exit as well. Pinned by `TestFlushingUsageGivesUpOnAStalledLedger`.

## The allocation laws

| Law | Where it is pinned |
| --- | --- |
| A warm tool-schema encode allocates **nothing**. The belt is append-only, so the memo hands back the slice it holds. | `internal/provider/alloclaws_test.go` |
| A warm transcript encode costs the **same** at 81 turns as at 8 — 1 allocation automatic, 8 with breakpoints. Encode runs once per call, so a per-call cost linear in the transcript is a per-run cost quadratic in the run. | `internal/provider/alloclaws_test.go` |
| The babble guard builds **one** zlib writer per stream and Resets it per window; a window costs at most 4 allocations. A writer per window is a hundred kilobytes of deflate state per five hundred bytes of reply. | `internal/provider/alloclaws_test.go` |
| The hub's backlog fold is **amortized constant per delta**: ten times the deltas for less than twice the allocations. `Text += delta` is quadratic — 1.6 GB of copying over one long reply, under the hub's lock. | `internal/session/alloclaws_test.go` |
| A frame with a four-thousand-line draft costs what a frame with a twelve-line draft costs. | `internal/tui3/inputsmooth_test.go` |
| Message-part reads and the v2 token formatters allocate nothing. | `internal/store/message_parts_test.go`, `internal/tui2/tokens/format_test.go` |

Correctness is pinned separately and deliberately so: `memo_test.go` proves the
memo's bytes are the direct path's bytes, `attach_test.go` proves the fold spells
the whole answer. Those tests say the fast path is *right*; the ones above say it
is still *fast*.

## The connection laws

Over `--host` the surface runs on the laptop and only the engine is far away
(docs/REMOTE.md), so every question the surface asks its agent is a round trip
down an ssh pipe with a ten-second deadline on it (internal/remote's
`callDeadline`) — and every one of them is made from the update loop, which is
the one goroutine that also decodes keys, resolves clicks and paints. A question
asked while DRAWING is therefore a question asked thirty times a second, and one
asked while resolving a POINTER is asked once per cell the pointer crosses.

| Law | Where it is pinned |
| --- | --- |
| **A frame over a connection asks the far machine nothing.** | `internal/tui3/hostlatency_test.go` |
| **A pointer motion over a connection asks the far machine nothing** — including one below the conversation, which rebuilds the chrome to find its row. | `internal/tui3/hostlatency_test.go` |
| **The model picker draws its whole list for nothing**, however many rows it is showing. | `internal/tui3/hostlatency_test.go` |
| **The frame clock's beat makes no call on the update loop.** What it reads it reads as a `tea.Cmd`. | `internal/tui3/hostlatency_test.go` |

`remote.Client.CallsMade` exists for these pins and for nothing else — one
atomic add inside the one door every call already goes through. It counts calls
and never stream frames, because a turn's events are the work a person asked for
and a getter is work nobody did.

They were written after the owner reported that over `--host` "even hover seems
to slow everything down", and that clicks and keys felt dead. It was one defect
in three places: the status row asked the agent what the model was dialled to
while it was drawing, a hover below the conversation rebuilds the chrome, and
the frame clock read the session's cost on the loop. At the twenty-millisecond
round trip a real link has, a pointer swept across the foot of the window put
about thirty-six milliseconds of network in front of the update loop per cell —
so a two-hundred-cell sweep left seven seconds of keystrokes and clicks queued
behind it, and a six-hundred-cell sweep twenty-two. Driven through tmux over a
pipe with that delay, a typed character took 7.4s to appear after two hundred
motions and 21.8s after six hundred; after the fix, 0.014s, which is what the
same script measures on a local session. internal/tui3's reasoninglevel.go holds
the fix.

The number that is NOT pinned here is the boot: opening a hosted conversation
costs six calls, one of them the current model's dial. Six is a launch cost paid
once with a person watching a connection open, which is the moment waiting is
correct; the laws above are about the moments it never is.
## The storm laws

The section above took the far machine out of the pointer's way. What is left is
the one cost every input message pays whether or not anything is far away: a
pointer swept across the window sends **one message per cell it crosses** — six
hundred for a fast diagonal — and Bubble Tea builds a frame after every one of
them. It writes one per sixtieth of a second, so nearly all of those frames are
built and thrown away, and the keystroke behind the sweep waits for all of them.

Measured on the loopback client through a pipe with a 20 ms round trip, with the
connection laws above already in place, delivering the whole sweep in **one
write** — which is what a terminal actually does, and what `tmux send-keys` in a
loop cannot reproduce because it paces itself at about 7 ms an event:

| A typed character appears after… | before | after |
| --- | --- | --- |
| no motion at all | 0.015 s | 0.015 s |
| 600 motions in one write | 0.045 s | 0.013 s |
| 3000 motions in one write | 0.204 s | 0.016 s |
| 600 wheel notches in one write | 0.047 s | 0.013 s |

The before column is linear in the burst — 0.07 ms a message, all of it frame
building — and the after column is flat. That is the point: **the cost of a
storm no longer depends on how big the storm is**, so a per-message cost added
back tomorrow cannot resurrect the stall.

`internal/tui3/coalesce.go` is the fold. The positions between the ends of a
sweep are not information; they are the same claim made six hundred times, and
every one but the last was already false when it was read. So the newest is
kept, the rest are dropped, and the surface answers **once per frame** —
`pointerEvery`, which is `frameInterval` and not a second cadence. A folded
message also declares the frame before it rather than building one, because it
provably changed nothing `app.View` reads; that half is the larger one, and it
is only reachable because the fold is what knows.

| Law | Where it is pinned |
| --- | --- |
| **Six hundred motions cost one answer**, and 598 of them are folded away. | `internal/tui3/coalesce_test.go` |
| **A key never waits behind a sweep.** A hundred keys behind a six-hundred-motion storm are handled without the router answering a single motion on the way, and each character is in the draft when its own `Update` returns. | `internal/tui3/coalesce_test.go` |
| **Keys are never folded and never reordered** — not against each other and not against the motions around them. | `internal/tui3/coalesce_test.go` |
| **A sweep is answered where it ended**, never at a position it crossed. | `internal/tui3/coalesce_test.go` |
| **A folded wheel run scrolls exactly as far as an unfolded one.** A scroll is a distance, so a folded run owes its whole length and spends every notch of it at the frame. | `internal/tui3/coalesce_test.go` |
| **A folded message builds no frame**, at a handful of allocations against a frame's tens of thousands of bytes. | `internal/tui3/coalesce_test.go` |
| A pointer ARRIVING somewhere is still answered on the spot, with no clock in between: only the SECOND motion in a row is a sweep. | `internal/tui3/coalesce_test.go` |
| A pointer crossing a row it is already on still leaves no stale entry, no dirty flag and no frame. | `internal/tui3/inputsmooth_test.go` |

**Why a fold and not a drain.** `internal/session`'s stream is coalesced by
taking events off a channel until it would block (`waitEvent` in `app.go`) —
the honest way, because the backlog is in hand. A Bubble Tea program has no such
channel to reach: `Program.msgs` is unbuffered and private, the input reader
hands over one message at a time and blocks until the model has taken it, and
the rest of a storm is unparsed bytes in the terminal's own pipe. There is
nothing queued to drain and no way to look ahead, so the fold is made forward
instead — keep the newest position, answer it on a clock of its own.

**What Bubble Tea already rate-limits, and what it does not.** Measured against
v2.0.8, not assumed: the renderer writes to the terminal on a 60 Hz ticker
(`startRenderer`), and `render(view)` only stores the view under a lock. But
`model.View()` is called after **every** message. A clean frame over a
twenty-turn conversation measures 31 µs and 19 KB, independent of transcript
length — the row list is cached (`app.visible`) and the chrome around it is not.
Thirty-one microseconds times six hundred is the middle row of the table above.

## The launch-path pins

Two costs can hold a terminal dark before anything is drawn in it, and neither
looks slow in review: a question put to the model catalog through the door that
waits (a `GET /models` with a fifteen-second ceiling on a cold cache), and a
packed corpus decompressed. Both are counted, in `cmd/aforge/launchlaws_test.go`,
against a catalog endpoint that refuses immediately.

- **Nothing on the way to the first frame unpacks a corpus.** Zero, for
  `--version` and for the whole chat launch. `packed.Unpacks()` is the reading.
- **Wiring a conversation's subharnesses asks the catalog one blocking
  question**, and it is not this surface's: `subharness.go`'s linear
  constructor, shared with the headless doors where waiting is correct. What
  `chatv3_subharness.go` asks for itself is zero — it reads the window through
  `catalog.Catalog.ModelsNow`, which answers nil while the catalog warms.
- **The whole launch asks sixteen**, and that figure is a ratchet, not a law.
  Fifteen of them come from `v3RunHarness` building the harness tool bridge
  eagerly, which arms the media hands, each of which asks which model would
  draw, see, speak or sing. Nothing in the first frame reads any of those
  answers. It is written down so it is a known debt rather than a discovery, and
  the only direction it may move without a conversation is down.

`catalog.Catalog.BlockingReads` and `packed.Unpacks` exist for these pins and
for nothing else. Each is one atomic counter behind a door that already existed,
because "the launch does not wait for the catalog" is a claim about the shape of
the code and a stopwatch would test the network instead.
