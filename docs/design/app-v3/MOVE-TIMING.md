# Moving a chat: where the seconds go

Measured 2026-11-16 for the owner's complaint that a take-back costing half a
megabyte of network took 19 s between two GCP e2-standard-4 machines (and 41 s
and 38 s for cold takes, 19 s with the holder offline).

## The rig

`TestMoveTimelineRig` in `cmd/codeaf/app_move_timeline_test.go`. It runs two
real `codeaf app` processes on one machine, two homes, a loopback relay, a
scripted model, and the engine daemon on, as a shipped `codeaf app` runs. It
drives the app's own wire exactly as the field measurement did: `agents.take
{cell, seen}`, then `chats.list` until the row is `here`. The sync cadence is
the shipped 5 s default, not the 50 ms the other take tests use. The folder is
the field's shape: thirty 10 MB random files and a git repository holding only
its README. The journey has three turns on A, a cold take to B, one turn on B,
a take back to A while A's app and its old session still run, then B takes the
chat again after A has quit. PERF.md ("Moving a chat") has the command and the
ceilings.

Each take prints one `MOVE` line with the wire's answer, `here`, the bytes the
relay served and the continuer's own `continue … phases` split. The finer
splits below came from temporary tagged logs at the engine transport and the
handoff phases. Those logs are removed.

## Take-back of a small change (600 MB folder, ~75 KB over the network)

| Phase | Before | After | What it was |
| --- | --- | --- | --- |
| app releases its old session (`releaseForTake`) | 0.05–0.1 s | same | real work |
| local capture, directory reads, lease (`acquire`) | < 0.03 s | same | real work |
| waiting for the holder to release or seal | 0 | 0 | none: the holder had published, and `retireHolder` found the session already closed (`retirement` 0.1 ms, `releaseWithin` never reached) |
| kept-branch seal and upload | 0.1 s | same | see "Still open" |
| want/import rounds (`fetch`) | 0.15–0.18 s | same | seven daemon round trips, real work |
| admission (`StageAdmission`: watch, rewind, forget) | **2.0 s** | 0.1–0.3 s | three one-shot engine processes, each re-verifying the whole pack |
| restore into the person's folder (`restore`) | **7.1 s** | 0.1–0.2 s | a secret screen re-reading all 600 MB before the in-place restore |
| arrival look and filing (`filing`) | 0.22–0.24 s | same | preflight and `git status` |
| app install, open, settle | 0.015 s | same | real work |
| **total, take to `here`** | **9.8 s** | **0.8–1.2 s** | |

## Take while the holder is offline (B's copy, ~40 KB over the network)

| Phase | Before | After | What it was |
| --- | --- | --- | --- |
| dirty check (`LocalWork`) | **9.1 s** | 0.8–1.0 s | the same full secret screen of the copy, plus one pack re-verify (below) |
| admission | 1.4 s | 0.1–0.3 s | the one-shot pack re-verifies |
| restore into the staging copy | 2.1–2.2 s | 1.4–1.8 s | the engine re-reads every file seeded from the copy, real work |
| filing | 0.22–0.28 s | same | |
| **total** | **13.0 s** | **2.6–3.6 s** | |

## Cold take (630 MB moved)

Cold takes did not change and should not have. The fetch takes 3.0–3.3 s: the
plan's frames come down eight at a time and are imported. The restore writes
600 MB in 1.3–1.4 s, and filing takes 0.25 s. The total is 4.8–5.7 s, bounded
by bytes. The field's 38–41 s for 606 MiB is this path over a WAN link with
`codeaf relay`, so it is bandwidth.

## Root causes, ranked by seconds on the rig

1. **Secret screen of unchanged files, 7 s of a 9.8 s take-back and 8+ s of
   an offline take.** A take derives a fresh engine (`cellstore.EngineFor("")`),
   and that engine had a clean-file ledger of its own. Before an in-place
   restore, and in the dirty check, `screenCapture` walks the folder with
   `Guard.Look`. With an empty ledger, that walk reads every byte. Fix:
   one process-wide ledger (`cellstore.cleanFiles`). A ledger entry is honoured
   only while the file's identity, mode, size and modification time are
   unchanged, so a file that the session's engine judged clean is equally
   trustworthy for the take's engine.
2. **One-shot engine processes re-verifying the whole pack, 2 s of a
   take-back.** The daemon writes with deferred durability, which leaves object
   rows beyond the pack checkpoint. A one-shot `furrow` process that opens the
   store treats that checkpoint as unusable and reads the whole pack again from
   byte zero. At 600 MB that is 0.65 s, and 1.25 s once the pack also held
   `.git`'s copy. It happens on every open until something settles the pack.
   Fix: `Spawn.Do` first asks a daemon that is already answering to `sync` the
   store (about 10 ms). It never starts a daemon.
3. **Nothing waited on a timer.** `releaseWithin`, the 5 s batch and sync
   intervals, `TailQuiet`, `holdsLeaseWithin` and `startWait` were all on the
   suspect list, and none appears on the measured path. The holder had
   published before the take began. The app closes its own old session before
   the continuer looks for it. The daemon was already running.

The field's 19 s fits the rig's 9.8 s on slower disks. Both large items are
read rates: a full read of the folder and of the pack, which an e2-standard-4
persistent disk does at a fraction of this box's NVMe. That is an inference
from the rig, not a field measurement.

The rig also showed a no-daemon mode (`CODEAF_ENGINE_DAEMON=0`) at 27.5 s.
There, every want and import round was a one-shot process paying cause 2.
Seals still use the daemon in that mode, so the settle applies there too.

## Still open

- **The dirty check walks the pack once (0.6–1.2 s at 600 MB).** The
  dry-run rewind it spawns appends to the pack itself, as a one-shot process.
  The next one-shot open then re-verifies the pack, and a daemon sync does not
  settle rows that another process wrote. This is an engine fix: verify from
  the checkpoint instead of resetting to byte zero when the rows past it are
  well formed.
- **Every take-back on the rig set aside a kept branch** (`kept_upload` above
  zero). The old session's closing write counts as local work. It costs 0.1 s.
  Whether it should appear to the person at all is a separate question from
  timing.
- **A turn made right after a take-back did not publish within 90 s** in two
  of four runs. That is `Drive.firstPublishOwed`'s area (#2654), so the rig's
  offline step takes the chat straight after the take-back instead of after
  another turn.
- **Two of about a dozen cold takes downloaded the plan twice** (1.26 GB for
  630 MB), apparently a priming that degraded into the rid loop. It did not
  recur in three instrumented runs.
- The restore into a staging copy re-reads every seeded file (1.4–1.8 s at
  600 MB). That is engine work proportional to size.
