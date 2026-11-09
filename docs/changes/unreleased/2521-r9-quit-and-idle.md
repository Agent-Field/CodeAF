---
kind: fixed
title: A quit mid-reply finishes in seconds, an idle quit exits 0, and idle factory writes stop
pr: 2521
surface: [engine, factory, chat]
invalidates:
  - "A quit waited for every cut stream's cost receipt on its full schedule (pauses of 1, 4, 15 and 20 s, up to the 70 s ReceiptWait), so a quit with replies mid-stream sat in provider.reconcile until the desk's 30 s kill (exit 143). Closing the app now hurries receipts (provider.HurryReceipts): a pause under way ends, each owed receipt is asked once more within receiptHurryAsk (2 s) and then recorded as unpriced with its tokens, and the app's quit waits at most appQuitReceiptWait (3 s) per conversation for them."
  - "A chat whose workspace folder was deleted while it was open (a finished job's checkout) failed its next capture, and the failure was reported at exit as 'codeaf could not save this chat' with exit code 1. A capture or seal of a folder that is gone is now cellstore.ErrWorkspaceGone, which WaitCapture and the final-save report treat as nothing to save."
  - "codeaf clock waited out its 30 s closingGrace after the window that started it closed, even with no other window open; the Orphaned check was only read while a window was open. An orphaned clock with no open window and nothing in hand now leaves on its next 2 s poll."
  - "The factory root watch re-read the root every 2 s (changePoll) even when fsnotify was watching it. A watched root is now re-read every 30 s (changePollWatched) as a repair for lost events; a root with no kernel watch keeps the 2 s poll."
  - "A followed AgentField run was written to its job on every reading (afRemoteEvery, 500 ms), with a new revision and Changed stamped now, even when the tree had not moved. A reading that leaves the job as it was now writes nothing, so a job's Changed is when its run last moved."
  - "A machine whose runner slots held two jobs was announced again (a machine and a runner live event, and a rewrite of live-events.json) on every heartbeat, because the roster row names the job heard from last. machineEvents keeps the job it announced while the machine still holds it."
  - "Every lease renewal (Desk.Heartbeat and Desk.Renew, every 2 s per held job) rewrote leases.json. A renewal is now written only once the stored renewal is lease.RenewalSettles (10 s, or a sixth of a shorter TTL) old; the renewal still answers its full new deadline, and a lease's stored heartbeat_at can be up to 10 s behind the runner's last beat."
  - "An idle chat's knowledge safety tick (every 5 s) opened its journal and asked for consent each time. A tick whose journal has the size and time a read that reached its end saw now does nothing."
---

From the ninth live WorkAF round (2026-11-08): two of three quits with jobs running hit the desk's 30 s kill, the idle quit exited 1 with "codeaf could not save this chat", the old clock stayed about 30 s after the engine, and the idle engine read 6.3–7.4% of a core on macOS. PERF.md ("Quit and idle, the ninth live round") has the rig and the numbers.
