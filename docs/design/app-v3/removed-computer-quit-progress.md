# Removed-computer quit progress

Implemented for the terminal chat surface. Quit progress reads the current
pairing’s confirmed removal before each sentence. Saving, ownership, and stored
history retain their existing contracts.

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

## Implemented change

`syncsetup.ConfirmedRemoved` reads the current pairing through the same removal
state used by the drive and wire gate. Read failures do not confirm removal;
a replacement pairing uses its own removal state.

Local quit progress rechecks that decision before each sentence. An explicitly
remote host keeps its engine's service independent of the terminal's pairing.
`chatSyncService` is unchanged, so the devices card and outage notices keep
their address; only quit progress consults removal.
All local saves, close replies, flushes, receipts and waits remain intact.

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
