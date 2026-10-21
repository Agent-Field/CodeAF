---
kind: added
title: paired computers continue the same chat with its workspace and history
pr: 0
surface: [chat, engine, remote, build, docs]
invalidates:
  - "A chat belonged to the computer that started it. A paired computer now lists it on home and can continue it with its files, history, saved tool output and task records; the previous window becomes view-only and unsent turns survive as a branch."
  - "Moving chats was described as on for every build. Cells now default on for dev, staging and source builds, off for rc and stable; CODEAF_CELLS=1 or 0 overrides the channel default, and Windows offers no pairing, identity or move capability."
  - "Opening the add-device instructions woke sync for good, though nothing was paired. A new home now sends no relay requests until it completes a pairing or offers a pairing code; showing a code uses the relay even if nobody joins, and a new device's codeaf pair contacts the relay to offer its link but syncs chats only after approval. Opening or closing the instructions, listing devices, showing or exporting an identity, declining and a failed approval leave it quiet. An identity made by an older build without the quiet marker keeps its earlier behaviour; CODEAF_SYNC_URL=off stops relay traffic there too."
  - "Secret files were kept out of the workspace copy by reading only their first MiB, and nothing said what happened to git history. The guard now reads every working file whole; a working file it recognises as holding a secret is kept out of the capture and travels only through the encrypted vault between paired devices. Git history, branches, settings and task commits travel as they are, sealed end to end to your own paired devices, even when an old commit holds a key: the relay sees ciphertext, and a key committed earlier stays readable in that history on those devices."
  - "Sync needed a relay setting. A paired computer now uses the relay saved by pairing or the hosted default. CODEAF_SYNC_URL=off sends nothing, including through clients opened before it was read, and pairing --via cannot override it; CODEAF_SYNC_URL=<url> sends relay traffic only to that address, such as a self-hosted codeaf relay; CODEAF_CELLS=0 writes no cell store."
  - "Adding a computer meant typing a six-digit code. A new computer can now run codeaf pair and show a link and four-digit check for explicit approval from an existing device; /pair and codeaf pair --code keep the code-and-three-words route."
  - "Devices appeared only after a later chat start and used the hostname alone. Pairing now writes each side's own device record immediately; --name, codeaf devices rename and n in /devices name this computer, and revoke removes another computer's relay access."
  - "A stopped or disconnected computer could look online for minutes. The live device list now marks it offline after 25 seconds of silence, within 30 seconds, and updates when it answers again."
  - "Continuing a chat could open a half-arrived copy and label its project as a copy here. An arriving chat now waits until the move completes, then opens itself; a project absent on this computer is labelled copy from another machine."
  - "Task journals were capped like job logs and the task working copy carried codeaf's own logs as edits. Journals now move whole, task history is rebuilt, and codeaf's working-copy logs stay behind; job logs and saved tool output keep the 1 MiB per file and 16 MiB combined caps."
  - "A pairing panel swallowed mouse selection and replacement was terminal-only. The panel now copies a selected code or c copies it alone, and /pair <code> --replace asks explicitly before replacing local chats."
  - "The architecture map needed a separate app. codeaf atlas and /atlas now share a named map registry, picker, draggable diagram and step-through flows, with every mark drawn through the terminal's shared glyph repertoire."
  - "A self-hosted Mac relay flushed every frame pointer separately. It now commits a frame as one durability batch; the other platforms keep their existing flush paths."
  - "A reused pairing nameplate could answer a previous pairing's client. Both relays now return a random mailbox generation and refuse a client carrying an earlier opening; clients without the optional header still work, and hosted nameplates also wait one mailbox lifetime before reuse."
  - "The hosted relay counted 20 new identities per whole IP address each day and stored ciphertext without a deployment-wide brake. It now allows 200 per IPv4 address or IPv6 /64, returns Retry-After, and adds operator-controlled daily global admission and total-storage ceilings while preserving existing reads and owner erasure."
  - "A wrong or expired pairing code could leave the joining computer waiting silently. It now ends with a sentence asking for a fresh code, or saying that the other device stopped pairing."
  - "Tests could leave engine daemons alive and pass their starter's open descriptors to them. Test binaries now stop their daemons, a crashed test's daemon has a one-minute idle limit, and engine processes inherit only their standard streams."
  - "Restoring a withheld file could follow a parent link outside the workspace. Vault file operations now resolve under opened directory handles, so replacing a parent name cannot redirect them into outside files."
  - "Release packaging could embed an engine without the exchange verbs. The engine now comes from Agent-Field/furrow v0.2.0, pinned in internal/furrowbin/pin.json; a missing artifact fails the build and an incapable runtime engine leaves cells absent."
---

Pair only computers you trust: they receive your shared identity and sealed vault.
Revoking a lost computer cannot take back its downloaded chats or keys; identity
rotation reseals the chats under a replacement and retires the relay's old copy.
The hosted worker's tests isolate local state from the caller's home. Its staging-first
operator runbook is relay/hosted/DEPLOY.md; deployment is performed by the owner.
