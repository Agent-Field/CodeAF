---
kind: fixed
title: Held leases stop an idle engine re-reading its floor, and closing chats share one save
pr: 2517
surface: [engine, factory]
invalidates:
  - "Every renewal of a held lease (every 2 s per hold) woke every open runner pull, and with any hold present each woken pull listed and deep-copied every job on the floor. A rewrite of leases.json that only moves heartbeat_at and expires_at (lease.RenewalShape) now wakes no pull, and neither do folders under the factory root or events.jsonl. An expired hold is still requeued by the next look, at most one pull wait (30 s) later."
  - "Each look (Desk.Pull, Desk.OverLimit) read the whole floor with ListSaved. The desk now keeps one sorted queue (Desk.floorQueue) shared by every slot: unchanged job files are reused, only a changed file is read again, and it is sorted only when something changed."
  - "A queued job already held at its budget with a manager reply pending was saved, unchanged, by every look, and each save woke every pull again. It is now saved only when the hold changes it."
  - "A runner's requests settled the floor at most once a second, which two held jobs' heartbeats made a full settle every second. A lease heartbeat or a presence beat now settles at most every 5 s (beatSettleEvery); pulls and reports still settle within 1 s (workerSettleEvery), a person's request still settles first and the job timer still settles at each deadline."
  - "A held job's runner parsed its manager's whole transcript twice on every 2 s beat (thread.Read). An unchanged journal (same file, size, time and folder) now returns the answer read last; an appended line is read."
  - "cellstore's group commit synced only the flushing chat's own records while every other closing chat waited behind its one slot. Whoever holds the slot now syncs every chat's owed records, each path once per pass; a chat whose engine snapshot failed keeps its records owed and is the one told."
  - "codeaf clock stayed alive for as long as any window was open, so an engine restarted by its desk kept the old engine's clock (and its binary). The clock now leaves once the window that started it is gone and no run is in hand; any open window starts the next within half a minute."
  - "CODEAF_DIAGNOSTIC_PROFILE_* could only profile the first 60 s of a process. CODEAF_DIAGNOSTIC_PROFILE_DELAY (whole seconds 1..3600) opens the window that long after start; nothing is created before it."
---

From the seventh live A+B run (2026-11-08): the engine idled at 20–26% of a core on macOS with 86 jobs, 15 runner slots, about 48 open chats and two native jobs holding leases.
