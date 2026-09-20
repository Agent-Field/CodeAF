# codeaf long-session decay: cause analysis (spark, 2026-09-20)

Binary under test: codeaf 4f002ca2, go1.26.5 linux/arm64, built from the clean checkout /home/santosh/src/benchhead (git status empty before and after this work). The instrumented runs used a fresh profile /home/santosh/src/profiles/footprint2-prof-codeaf, this work's own stub on 127.0.0.1:8094 (STUB_TOKENS=40, STUB_DELAY_MS=5, /home/santosh/src/stub.py), private tmux sockets footprint2-prof-$$, 25 stub turns each, two runs (default GC and GOGC=100). Teardown on both runs ended SURVIVORS=0 with no profile processes left, and box load1 (13.5 to 15.6 during the runs, shared with sibling benchmark tasks) is recorded per turn in prof/prof-turns-*.tsv. Nothing under /home/santosh/src/benchhead was modified and nothing was rebuilt.

## Answer in one paragraph

The +15.2 MB of RSS over 100 turns is not a leak and not live data: it is GC headroom plus freed-but-resident pages, in both processes. The chat TUI raises its own GC target at startup (cmd/codeaf/main.go:142, debug.SetGCPercent(400) in tuneForTheSurface), so a live set that stays flat at about 5 MB carries a 28 MB heap goal; measured, the TUI climbed 43.1 to 67.6 MB RSS over 25 turns with live heap flat, and the identical run with GOGC=100 climbed only 42.2 to 49.2 MB: one constant accounts for 18.4 MB of RSS at turn 25. The daemonized engine keeps the Go default (its dispatch case calls no tuner, main.go:270), and its growth is high-water creep: its live heap is flat at 3 to 4 MB (gctrace captured in its own host.log) while RSS creeps about 200 KB per turn early and 20 to 50 KB per turn later, which is per-turn allocation churn whose freed pages the runtime returns slowly. On disk, the 39 KB per turn figure is 97 percent SQLite WAL frames from the store's second copy of every message (about 135 KB per turn until a 4,157,112 byte WAL ceiling, then 2.8 KB per turn), not session records: the transcript itself is 599 B per turn.

## The growth curve, characterized

From decay/codeaf/samples.tsv (union of the TUI and the engine, six checkpoints, 100 turns):

| span | KB grown | KB per turn |
| --- | --- | --- |
| turns 1 to 10 | 4,912 | 546 |
| turns 10 to 25 | 1,944 | 130 |
| turns 25 to 50 | 3,732 | 149 |
| turns 50 to 75 | 3,216 | 129 |
| turns 75 to 100 | 1,672 | 67 |

Shape: a front-loaded ramp with a long, slow linear tail; no discrete steps, and turns.log records nothing but load1, so growth tracks turns, not turn events. PSS grows by the same +15.2 MB as RSS (70,966 to 86,148 KB), so the growth is private anonymous memory, not shared or file-backed pages.

## Per-process attribution (instrumented 25-turn run, default GC)

driver-prof.sh sampled both pids every turn (VmRSS plus smaps_rollup from /proc). Per-turn series are in prof/prof-samples-gctrace.tsv; the shape in three numbers each:

- TUI (pid 3671782): RSS 43,080 KB at turn 0, 50,080 at turn 1, 67,564 at turn 25. The ramp is concentrated in turns 0 to 8 (+20,144 KB), after which it creeps near 100 KB per turn (turns 17 to 25: +776 KB). Pss_Anon grows 13,660 to 36,668 KB while Pss_File stays flat at 5,817 to 6,358 KB.
- Engine, the daemonized codeaf engine (pid 3671828): RSS 66,188 KB at turn 0 (bootstrap spike, see cause 3), 52,900 at turn 1 after the first GCs, 57,972 at turn 25: +5,072 KB from turn 1, of which +2,744 KB lands by turn 8 and only +172 KB over turns 16 to 25 (about 20 KB per turn late). Pss_Anon 18,476 to 23,084 KB; Pss_File flat at 6,412 to 7,115 KB.

Both processes: all growth is anonymous, file-backed pages do not grow, and one to two threads appear over the run (stack cost trivial).

## Heap versus headroom: the direct measurements

GODEBUG=gctrace=1 was exported for both runs; the daemon inherits it and its stderr lands in its host log (internal/enginehost/enginehost.go:408-429 wires stderr to that file at :416-422).

- TUI, 11 GCs: live heap after each GC is 4 to 5 MB for the whole run (gc 5: "24->24->5 MB, 26 MB goal"; gc 11: "27->27->5 MB, 28 MB goal"). Heap at trigger climbs to the goal and stays there: the goal is live x 5, the SetGCPercent(400) target. RSS grows 24.5 MB while live grows about 1 MB.
- Engine, 25 GCs: live heap 3 to 4 MB flat (gc 23: "7->8->4 MB, 8 MB goal"); goal 8 to 9 MB, the Go default 2x. RSS climbs 5.1 MB from turn 1 with live flat: the gap is heap arenas touched at peak and returned slowly (Pss_Anon 23.1 MB at turn 25 against a heap never above 9 MB).

The A/B run (one environment variable, GOGC=100; tuneForTheSurface honors an explicit GOGC, main.go:66 and main.go:141):

| measurement, turn 25 | default (GOGC 400) | GOGC=100 |
| --- | --- | --- |
| TUI RSS | 67,564 KB | 49,152 KB |
| TUI Pss_Anon | 36,668 KB | 18,032 KB |
| TUI live heap (gctrace) | 5 MB | 5 MB |
| TUI heap goal (gctrace) | 28 MB | 11 MB |
| Engine RSS | 57,972 KB | 58,424 KB |

Same live set, same turns, same disk behavior: the TUI's 18,412 KB lower RSS at turn 25 is the GC headroom policy alone, and the engine is unchanged because it never ran the 400.

Reconciling with the decay run: the decay box ran at load1 3.9 to 6.5 with turns about 3.1 s each; the instrumented box ran at load1 13.5 to 15.6 with turns about 5.6 s each, and the TUI's per-turn allocation churn (a frame is built per surface message, internal/tui3/app.go:1385-1393) scales with that time, so the same ramp stretched across the decay run's 100 turns. The decay total of +15.2 MB is the TUI ramp toward its 28 MB goal (the bulk, and bounded by it) plus the engine's smaller ramp and creep, both headroom class, neither live growth.

## Confirmed causes

1. TUI GC headroom policy (dominant). cmd/codeaf/main.go:134 tuneForTheSurface; main.go:142 debug.SetGCPercent(400) whenever GOGC is unset; called for the no-args default (main.go:257), chat (main.go:262) and resume (main.go:268); the engine case (main.go:270) calls no tuner, so the daemon keeps GOGC=100 (the source says so at main.go:62: do, run, exec, engine and a subharness run headless and keep the default). Evidence: flat 5 MB live heap against a 28 MB goal, and the 67.6 versus 49.2 MB A/B above. This is a deliberate, documented trade (main.go:55-65: trading resident memory for fewer collections where a person is watching), with a soft memory limit as the other half (main.go:146, half the machine bound: about 59.7 GB on this box, so it never binds here).

2. Engine freed-but-resident creep. The engine holds the open conversation's messages in memory for as long as it is open (internal/session/agent.go:2381-2386 recordLocked: append to a.messages, append to the journal file, post to the store; recordUserLocked at :2402-2421 does the same for the person's messages) and re-sends the whole transcript on every step of every turn (the sessionCompleter comment above internal/session/agent.go:2092). Retention is not the growth: the measured live set is flat at 3 to 4 MB. The growth is per-turn churn, and a trivial stub turn still makes 5 provider calls (calls.jsonl over the decay run: 2 reflex plus 2 router plus 1 turn call per turn, title once per session, started at internal/session/agent.go:1678, recall routed per turn at :1690), so the engine allocates and frees several hundred KB per turn, and freed heap pages stay resident.

3. Engine bootstrap high-water. At turn 0 the engine holds 66.2 MB RSS: openEngineProcess opens the store (internal/store/store.go:749 journal_mode=WAL, then schema, thread, FTS, session, usage, turnUsage, transcript and surprise schema plus backfills, store.go:757-800) and the model catalog and graph snapshot that the launch path keeps (main.go:57-60 comment). The first GCs bring it to 52.9 MB by turn 1. This sets the turn-1 baseline, not the slope.

4. Disk: the store's second journal of every message. Every message that lands is appended to a.messages, appended to the session's transcript.jsonl, and posted to the sqlite store's thread (agent.go:2381-2386; internal/session/chatlog.go:139 post, queue field at :98, queue append at :145, the writer goroutine draining batches at :225-236; the store is the same graph.db every surface opens, chatv3.go's v3Memory). Measured per turn until the ceiling: 131,840 to 156,560 bytes of WAL per turn (mostly 131,840 = 32 frames of 4,120 bytes), starting from a 1,116,552 byte bootstrap WAL at turn 0, reaching the 4,157,112 byte ceiling at turn 22 to 23, where sqlite's autocheckpoint moved 495,616 bytes into graph.db (4,096 to 499,712) and the WAL stopped growing. After the ceiling the per-turn disk cost is 2,807 bytes: transcript 599 plus calls.jsonl 2,208.

   The decay du deltas reconcile with this model to within 0.01 percent: turns 0 to 50 modeled as WAL 3,040,560 + checkpoint 495,616 + transcript 29,950 + calls 110,400 = 3,676,526 B against 3,676,356 B measured; turns 50 to 100 modeled as transcript plus calls plus meta rewrites (WAL flat at the ceiling) against 245,779 B measured.

5. RSS double-counts the shared binary (presentation, not a growth cause). Both processes map the same 53 MB codeaf binary; at decay turn 100 the RSS sum is 128.6 MB but PSS is 84.1 MB: about 44 MB of shared text counted once by PSS. Growth is identical in both metrics (+15.2 MB), but cross-CLI comparisons against single-process CLIs (omp, claude, opencode) should use PSS.

## Refuted hypotheses

- Transcript retained and re-processed in memory (H1): retained, yes (agent.go:2381-2383, plus the TUI's display copy at internal/tui3/app.go:913, transcript []session.DisplayEntry), but re-processing is not per-turn: the journal is appended each turn and read only at open (internal/session/sessionfile.go:1827 readJournal), and the retained size for 100 stub turns is about 60 KB (599 B per turn of journal). The measured live heaps are flat at 3 to 4 MB (engine) and 4 to 5 MB (TUI). Not the growth.
- GC headroom rather than a leak (H2): confirmed, and it is the dominant cause (causes 1 and 2 above).
- Per-turn retention of tool results, command outputs or provider responses (H3): no tools ran in the stub turns, both live heaps stayed flat, and the per-turn records that do exist (calls.jsonl rows, usage rows, chatlog refs) are small structures inside that flat live set.
- Caches warming in the first 25 turns (H4): the front-load is the heap ramping to its 5x goal plus the bootstrap WAL, not caches: a flat 5 MB live set bounds every warm cache in the TUI, and the flat 3 to 4 MB live set bounds the engine's. The prior lexer work (perf-lexer, 7.3 ms at startup) is startup latency, not retained memory.
- mmap of growing journal files (H5): killed by per-turn smaps_rollup: Pss_File is flat at 5,817 to 6,358 KB (TUI) and 6,412 to 7,115 KB (engine) across 25 turns, and all growth is Pss_Anon. The journals are written with ordinary file writes (sqlite and os.File), not mapped.
- Disk 39 KB per turn as session state (H6): decomposed in cause 4: about 135 KB per turn of WAL frames until the ceiling, then 2.8 KB per turn of actual records. Nobody reads the WAL back; it is redo. Codeaf's post-ceiling steady state (2.8 KB per turn) is the same class as claude's measured 5 KB per turn; what claude plausibly does that codeaf does not is keep exactly one durable copy of the conversation, where codeaf keeps two (transcript.jsonl plus the sqlite thread) and pays the WAL churn on the second.

## What compaction or vacuum would cost

- WAL: PRAGMA wal_checkpoint(TRUNCATE) on an idle path, or a lower wal_autocheckpoint so the WAL stays near 400 KB. The checkpoint is one pass over at most 1,009 frames (4.2 MB) into a 0.5 MB database: milliseconds, occasionally. The standing ticker already exists as the idle hook (internal/standing/tick.go:38 Tick).
- The store's main file needs no VACUUM: it is 499,712 bytes after the one checkpoint.
- Compaction (context-triggered; it rebuilds a.messages in memory at internal/session/loop.go:4935, and rewind trims at internal/session/rewind.go:186-191) never fires in stub turns because 40-token replies never reach the threshold, and compaction is a context feature, not a memory fix: the live set is already flat.

## Fix table

| # | cause | fix sketch | easy or hard, and why | expected recovery per 100 turns | risk |
| --- | --- | --- | --- | --- | --- |
| 1 | TUI GC headroom, GOGC=400 (main.go:142) | lower the surface GC percent (to 100 or 200) or make it adaptive; an explicit GOGC already overrides it (main.go:141), and a tighter SetMemoryLimit is the other lever the file already names | easy: one constant in one function, and this A/B is already measured; but the 400 is a documented trade (main.go:55-65), so the perf gate (first paint, scroll) must be re-run before shipping | 10 to 15 MB RSS (measured: 18.4 MB less at turn 25 in the A/B) | more collections during rendering; the source's own comment says why it chose 400 |
| 2 | engine freed-but-resident creep | optional debug.FreeOSMemory on the idle tick (standing/tick.go:38), or accept it: it self-limits near 58 MB RSS | easy to add; partial effect: it returns freed pages, but the next peak re-touches them | 2 to 5 MB RSS | a forced GC plus unmap on a busy path adds visible latency; must be gated on idle |
| 3 | sqlite WAL ceiling churn (store.go:749) | wal_checkpoint(TRUNCATE) on the idle tick, or wal_autocheckpoint(100) | easy: one pragma on an existing idle path; the checkpoint is one small pass | 3.0 to 3.5 MB disk | negligible at this write rate: a few more small checkpoints |
| 4 | 5 provider calls per trivial turn (2 reflex + 2 router + 1 turn, from calls.jsonl) | skip reflex and router when the turn's input cannot change the routed memory or the reflex answer | hard: a product behavior decision crossing the router and memory features, not a memory patch | small RSS (churn only); 1.7 KB per turn of calls.jsonl; 5x fewer model calls per turn | changes routing and memory behavior; needs its own benchmarks |
| 5 | RSS double-count of the shared binary | none: compare CLIs by PSS (the decay table already carries both); codeaf engine --no-host (engine.go:82) exists when one process is wanted | not a defect, presentation only | none | none |

## Top three causes

1. TUI GC headroom: the chat surface raises GOGC to 400 (cmd/codeaf/main.go:142), so a flat 5 MB live set carries a 28 MB heap goal; measured flat live heap with RSS +24.5 MB in 25 turns default versus +7.0 MB with GOGC=100.
2. Engine high-water creep: the daemon's live heap is flat at 3 to 4 MB (gctrace in host.log) while RSS creeps about 200 KB per turn early and 20 to 50 KB per turn later; freed pages stay resident.
3. Disk double journaling: every message also posted to the sqlite store (agent.go:2381-2386 via chatlog.go) turns into about 32 WAL frames (135 KB) per turn until the 4,157,112 byte ceiling; the transcript itself is 599 B per turn.

## Raw evidence

- Decay run (100 turns): /home/santosh/src/footprint2/decay/codeaf/ (samples.tsv, du.tsv, summary.md, turns.log, driver.log) and the profile it left, /home/santosh/src/profiles/footprint2-decay-codeaf.
- Instrumented runs (25 turns each, default GC and GOGC=100): /home/santosh/src/footprint2/decay/prof/ with per-turn per-pid samples (prof-samples-gctrace.tsv, prof-samples-gogc100.tsv), per-turn file sizes (prof-files-*.tsv), du (prof-du-*.tsv), per-turn load (prof-turns-*.tsv), gctrace for both processes (tui-stderr-*.log, engine-stderr-*.log), stub logs, teardown records ending SURVIVORS=0, and driver logs. Driver: /home/santosh/src/footprint2/decay/driver-prof.sh (runner: run-prof-all.sh), modeled on the shared decay template's /proc walk and finish detection.
- All file:line citations are against /home/santosh/src/benchhead at 4f002ca2, working tree clean throughout.
