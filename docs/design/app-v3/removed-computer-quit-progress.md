# Removed-computer quit progress

Proposal for the terminal chat surface. It proposes a quit presentation change
that requires review before implementation. Existing contracts still govern
saving, ownership, and stored history.

## Problem

`TestL4HostedRemovedComputerQuitsQuietlyAndKeepsLocalHistory` requires a removed
computer to finish its local save quietly. A delayed hosted close reply can
instead produce both of these lines after local durability has succeeded:

```text
saving this chat to your other devices…
still saving — ctrl+c leaves it saved here
```

The progress reporter does not read another computer's pending state.
`cmd/codeaf/final_save.go` starts progress whenever the captured sync address is
nonempty and is not `off`. Its timers print the first line after 150 milliseconds
and select the second line three seconds later using only local durability.
Neither decision checks whether this computer has been removed.

`cmd/codeaf/syncdrive.go` supplies that address through `chatSyncService`, which
checks pairing quietness but does not check removal. Removing a computer leaves
its identity and service setting available, so an address remains even though
sharing has stopped. `cmd/codeaf/chatv3_surface.go` keeps the reporter running
while joining hosted close replies and subsequent cleanup.

The quiet-removal test arrived in `cc1aaa80b` (merged by `d90273989`). The timed
progress reporter arrived in `20eb6952d` (merged by `558a3c830`). These establish
the conflicting behaviors; they do not identify which cleanup operation was
slow in a particular full-suite failure.

## What removal already does

Pending publication lives in the drive's `cellsync.Batcher`, not on another
computer. `internal/syncsetup/drive.go` refuses new publication notes once removed.
Its close path skips `Batcher.Close`, cancels the background run, joins its
workers, retains local durability errors, and ends its publication requirement
after successful close. `internal/syncsetup/quiet.go` prevents new relay requests
from a removed handle.

A stalled publish response alone can therefore be canceled successfully without
printing progress. Delaying the hosted close acknowledgment after `closeCell`
returns reproduces the incorrect lines without requiring stale publication debt.
This proves that pending publication is unnecessary for the presentation defect.
It does not prove which delay caused the original load-sensitive failure.

## Proposed change

Treat eligibility to share as an input to quit progress, separate from the
local durability receipt. For a local chat whose current pairing is confirmed
removed, suppress the cross-device progress sentences while retaining every
existing local save, close reply, flush, and error check.

Start with the smallest local seam: have the local service presentation decision
account for the current pairing's confirmed removal. A read failure must not
silently imply removal. Keep an explicitly supplied remote engine's service
independent of this terminal's pairing. Resolve removal against the current
pairing so adding this computer again restores ordinary progress.

If removal during an already-running quit must also be covered, recheck sharing
eligibility before each progress sentence. Do not clear pending turns, forge a
local receipt, bypass a worker join, or shorten a durability fence to silence
the output. Whether delayed local-only saving needs its own progress sentence
must be settled against the existing quiet-removal contract before implementation.

## Acceptance

1. Keep the existing hosted quit output and retained-history assertions intact.
2. Stall a publish response before removal, then block the hosted close reply
   after its local close succeeds. Release the reply after both progress timers
   have elapsed. A removed computer must remain quiet and retain its full history.
3. Keep ordinary paired progress, local-save failure reporting, and progress
   after pairing again covered at the same observable seams.
4. Run the named regression repeatedly and the neighboring hosted quit tests
   under CPU load. Run affected packages and reverse importers in full under
   the coordinator's test lock before queueing.

No budget, timeout, ownership rule, or saved format needs to change. Implementation
must update the shipped chat manual and add a change entry for the visible result.
