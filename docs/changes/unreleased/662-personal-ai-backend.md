---
kind: added
title: Resolve organized work and share sourced context across chats
pr: 662
surface: [chat, engine, docs]
invalidates:
  - "Collections used to contain bare references reachable only through the CLI. Chats can now find and organize collections and resolve members through their existing owners."
  - "There was no durable sourced-context record or turn integration. Schema v2 adds immutable revisions, withdrawal and explicit targets; chats and ordinary task workers refresh bounded information independently of learned memory."
  - "The completion reader previously judged report fields from a clipped write label and a byte-count receipt, and its objection was presented as fact. The newest bounded exact write/edit input now accompanies its result; missing evidence is explicit and objections must be checked against current work before editing."
  - "A finishing standing check or action could overwrite a concurrent pause, stop or configuration edit. Revision-aware owner operations and runtime-only writeback now preserve newer intent and completed occurrence progress."
  - "Organization opens no longer reserve the writer for a current schema, and reads cannot recreate a missing database. Task workers can inspect but cannot reorganize or revise shared context."
  - "Ongoing work could only be set up, changed or inspected through the chat card. `aforge standing add|edit|show|pause|resume|stop|check` does it from a terminal, and `aforge collections place|unplace` sets governing folders there."
  - "A standing order could not be edited after it stood; a change meant stop and a new card. `aforge standing edit` makes a new instructions version (`specRevision`) that the next run uses."
  - "A standing run's journal recorded `parent_cause: not_recorded`. Task firings now write `occurrence.json` before they run and their journal names that occurrence as the cause."
  - "A file watch told its run the whole listing. It now says which files were added, modified or removed since the previous reading."
  - "A process killed mid-firing re-fired the same change with no link to the half-done run. The retry is `attempt 2` of the same occurrence and names the interrupted run; a finished but unrecorded run is not run again."
  - "One standing pass could last 120 seconds. It may now last one interval (5 minutes), and a run cut off mid-answer is failed rather than landed."
  - "Standing item documents are schema 4 (specification revision and report path). Older engines refuse them; stop old tickers and windows before upgrading."
  - "A rule placed on a folder only reached a standing run's instructions; nothing checked what the run published. A report is now read against the rules that reached its run before it is published, sent back once on a quoted finding, and held back (draft kept, previous report unchanged, the item waiting on the person) if it still breaks one. It is a model's reading, not a proof."
  - "`aforge standing check` exited 0 whatever its runs came to. It exits 2 when a run did not finish and 4 when one is waiting on the person."
  - "A standing run stopped at its step or spending limit ended its turn normally and published whatever report it had written, and a report opened and never closed was published to wherever it stopped. Both now fail and publish nothing, as does a run that tried to write its own report file and never replied with one; the previous report stays."
  - "A stop while a standing run was working still let it replace its report and send a note. A stopped item's in-flight run now publishes nothing and sends nothing; what it already did is not undone, and a pause still lets it finish."
  - "A standing run replying `<report>` and `</report>` with nothing between them replaced the last good report with an empty file. An empty report is no report: the run fails with `the run's report was empty` and the previous report stays."
  - "A standing run whose answer ran out of output-limit continuations published the partial answer, and so did its one rules correction. The turn's ending now says it was cut (`Truncated` on the turn-done event), and the run fails with `the run's answer was cut off at the model's output limit`."
  - "A standing answer with a `</report>` but no `<report>` line was published whole, stray tag and all. It is no longer published (`the run's report has a closing line but no opening line`)."
  - "A withheld standing report said why only in words. `occurrence.json` now carries `withheld` — a fixed kebab-case code (`output-limit`, `at-a-limit`, `empty-report`, …) — on withheld runs only, and `aforge standing show` prints it on the `came to:` line. Older records without it stay valid."
  - "A standing report written in a turn cut at the output limit was published if a later turn ended clean with no report of its own (a divided run's \"Acknowledged.\"). A report now carries how the turn that wrote it ended, and only a new report stands in for a cut one."
  - "A standing order with no report was announced as landed when its run was stopped at a step or spending limit or its answer ran out of output. It now comes to failed with `at-a-limit` or `output-limit`, and `withheld` is recorded for any run that did not come back clean, report or not."
  - "A stop or a pass ending between the publish decision and the write still published, because the writer read neither. The report's rename and the note's delivery each recheck both at the act, the stop under the item's own lock."
  - "A standing report replaced the file whatever it held, losing edits a person saved during the run. It is replaced only if it is still what aforge last published there (the receipt's sha256) or is absent; otherwise the run waits on the person (`report-changed`) with its draft in `held-report.md`. A file that existed before aforge's first publication is treated the same way: move it aside to let aforge publish."
  - "Report delimiters were matched anywhere: a `</report>` inside a code fence cut the report short, and an inline mention refused an undelimited answer. They are now whole lines outside fenced code blocks, a leading byte-order mark is ignored, and a closing tag glued to other words is not a closing line."
  - "Standing run folders were numbered from the highest folder on disk plus one and read newest-first by string order, so a reaped newest run's number was reissued and past run 9999 recovery read the wrong records. Numbers now come from a recorded per-item counter (`last-run`), new folders are six digits, and runs are ordered by number; older four-digit folders read unchanged."
  - "A session's and a project's inbox, and a session's answers file, were deleted the moment they were read, and a staged `*.draining` file left by a crash was never read again. Inbox notes now carry an id and are removed only once the conversation's journal records the fold; answers are removed only once the journal records them (round 3b, below); staged files are re-read at the next open."
  - "A torn last line in a conversation's journal swallowed the next line written after it, and the journal was synced only on close. A torn tail is now moved to `transcript.jsonl.torn` on open, and the journal is synced at each turn's end and before a delivery settles. Standing documents, receipts and the run counter are written with a file and folder sync."
  - "An unattended standing run carried every tool and was refused the ones the approval rules did not allow, and any such refusal (`refused in a task: default — nobody to ask`) turned a finished report into a question no door answers. A run that fires with nobody watching now carries only the tools the rules grant without asking — no `write`, `edit` or `bash` under the default rules — and its prompt says so; a call to an absent tool answers `Unknown tool`."
  - "An unattended standing run's `read`, `ls`, `grep`, `find`, `read_document` and `view_image` reached the whole machine, including aforge's own home and other conversations' transcripts. They now reach only the item's workspace (symlinks resolved), refusing anything else with `… is outside this work's project (…)` while the run goes on."
  - "The read-only `collections` (list, show, find, governing) and `shared_context` (list, read, history) actions asked for approval and were refused in a task. They are now looks, allowed without asking wherever reads are."
  - "The rules check asked only whether a report broke a rule, so an omitted obligation had nothing to quote and was recorded `kept`. It now answers each rule by kind — an obligation is kept only with a quote showing it, a prohibition broken only with a quote — or `not-checkable`; `ruleCheck.verdicts` records each by rule id (`obligation`/`prohibition`, `kept`/`broken`/`not-checkable`), `ruleCheck.verdict` sums them, and `standing show` prints one line per rule. Older records stay valid."
  - "A standing run was told the conditions over its place but not which folders its work was placed in, and re-judged from a rule's wording whether it applied. The run is now told its placements, and each rule reaching it through a folder is marked with that folder."
  - "A standing run that spent past its per-run limit recorded only its cost. `occurrence.json` now keeps the limit (`perRunUsd`) and `standing show` says by how much the run went over it."
  - "`**` in a file watch behaved as `*`, so nested edits never woke it. A whole `**` segment now reaches every folder below, and a watch reaching more than 10000 files and folders is refused at setup and edit."
  - "A file touched or rewritten with identical contents woke a paid run. When a file's size or time moves its contents are hashed and compared with the last reading (files up to 4 MiB); the same contents are not a change. Readings kept before hashes existed are read quietly."
  - "Round 3b: the fence around a standing run's report and note sampled the pass's cancellation when it was made, so a pass cancelled after the decision and before that moment still published, and a cancellation while the file was being compared was never read again. The context is now read inside the item's lock immediately before each act. A store that cannot be opened or an item lock that cannot be taken used to let the act run unfenced; it now withholds with the new code `stop-unknown`. A note held by a cancellation left the run coded `cut-off` while its kind said landed; a clean run with no placed report is now `failed · cut-off`, and one whose report was already placed stays `landed` with no code and says its note was not delivered."
  - "Round 3b: a first standing report was renamed over whatever was at the path by then, so a file created after the look was written over. A first publication is now a hard-link create that refuses a taken name (`report-changed`); a filesystem that cannot link refuses the first publication. A replacement is NOT a compare-and-swap: it is compared a few microseconds before the rename, and an editor's save landing inside that window can still be written over. A held draft that could not be written is said on the line (`the draft could not be kept (…)`) instead of being left out."
  - "Round 3b: an answer left on a session's doorstep from home was applied and its file deleted with no durable record, a restart replay could not tell a taken answer from a new one, and a resolver's refusal was dropped. Each answer now carries a `delivery` id, is recorded in the journal as an `answer` line (taken, or refused with the reason) that is synced before the doorstep is cleared, and a replay skips any delivery the journal already holds. `DrainAnswers` takes an `apply` that returns an error and returns every refusal."
  - "Round 3b: a failed journal sync was ignored and the settlement that consumes an inbox went ahead, and a failed folder sync after an atomic write was read as success. A failed journal sync now stops every settlement for the rest of that file's life (the next open settles from what the disk holds), and an I/O error from a folder sync fails the write; a filesystem that cannot sync at all (`EINVAL`/`ENOTSUP`) is still accepted."
  - "Round 3b: the set of inbox notes an agent had folded grew for the life of the session and was walked under the agent's lock on every surface attach; it now holds only folds whose record has not settled. `Store.LastPublication` read every run record an item ever kept; it now reads the item's `published.json` receipt, falling back to the newest 64 runs by number from the run counter."
  - "Round 3b: tools a connected account arms through `use_service` bypassed the unattended grant filter. Arriving tools are now asked for their grant like the rest of the belt, and an account whose tools are all ungranted says so instead of loading nothing silently."
  - "\"Let aforge decide\" pressed while a turn's last request was already out was handed straight back by that turn's end, which never read it: the card drew its chips again while the next turn was told the person had asked the model to decide, and a second press was taken as fresh and sent the model a second copy. A press is now held until a request carries its note, the turn that reads it is the one that hands it back, and a press no turn is going to read (interrupt, spend rail, close, abandon) comes back to the person at once. Two presses racing each other can no longer both be taken. An abandoned turn unwinding later handed back the decision the turn after it was reading; it now hands back nothing, and the abandon itself returns what that turn held. A press during a picture answer from another model comes back when that answer ends. The press's card update is sent before its note is queued, so a give-back can no longer be followed by an older `aforge is deciding`. A turn let go of can no longer drain the next turn's queue or take back a press another turn read, and abandoning a picture answer no longer panics with `close of closed channel` when it comes unstuck."
  - "`aforge collections show --json` printed an array of references and `find --json` an array of collections. Both now print `{\"references\": [...], \"placed\": [...]}`, and the text output lists work placed in a folder (show) and the folders whose rules reach a record (find) under their own labels."
---

`collections` and `shared_context` are wired through the production binary.
The adapter reads existing conversation, task, ongoing-item and artifact owners;
the collection database does not become a second owner of execution state.
Snapshots preserve the cached system prefix, carry current provenance and
retire stale applicability on reopen. Paging and exact-revision text windows
bound context reads. Identity reads now distinguish globally readable records
from revisions currently applicable to the consumer. A removed membership must
not be restored merely because the completion check can still read the old ID.

`make test-organization-live` builds the binary and runs real DeepSeek V4 Flash
journeys with disposable data. It requires credentials and retains JSON receipts.
The matching CI workflow reports NOT RUN when its repository secret is absent.
The suite also exercises twelve chats with concurrent turns and longer review
history, observed idle behavior, conflicting sources versus a local draft choice,
and a paginated reference beyond the automatic snapshot. These are tests of the
existing behavior, not new activation or accepted-decision semantics.

This is the first integrated backend slice, not autonomous consultation,
semantic discovery or a dashboard redesign. Scheduled context inheritance is added in the governing-context wave below. The
continuation and acceptance record is `docs/design/workspace-foundation/HANDOFF.md`.
PR #662 remains draft and unmerged at the user's request.

Completion evidence distinguishes attempts from success and retains the existing
digest budget. Completion comparisons no longer share the sketch's 300-token
generation ceiling; the existing wall deadline still applies. Real-reader
positive and negative probes accompany the full organization journeys.

The completion follow-up also distinguishes full paths in person-facing file
references from workspace-relative file-tool arguments. The tools already
resolve relative paths; the old blanket full-path instruction encouraged
manual copying of long roots, and live working-day reports landed in mistyped
sibling folders. Explicit supplied/tool-returned paths and approval boundaries
are unchanged. The conflict fixture compares file identity, accepting symlink
aliases while continuing to reject a different or missing report.

The continuation now distinguishes agreed product direction from unresolved
design. An independent real-model behavioral audit retained false-success and
watch-recovery findings alongside passing isolation checks. Its exploratory
fixtures remain on a separate audit branch; they do not establish autonomous
global watching or alter the existing `/land` contract. No audit findings are
claimed fixed by this documentation update.

The current-dev baseline `996117314` is integrated while retaining the draft's
organization/context foundation and newer upstream completion, history and
control behavior. Grooming/design records and the live checklist are consolidated
into this draft; superseded #663 is closed only after its history is retained.

Standing item schema 2 adds stale-write detection and atomic narrow controls.
Runtime writeback preserves configuration, while schedule-specific reconciliation
consumes completed occurrences even after unrelated edits. Old engines/tickers
must be stopped and restarted together before using schema 2; mixed-version writers
are unsupported. This is not external cancellation, rollback or exactly-once effects.
The user has deferred expensive tui3/broad UI acceptance; the checklist records
focused functional Spark receipts and remaining target capabilities separately.

The governing-context wave adds explicit governing folder bindings without
promoting old references, folder-scoped holds with opt-in descendants, and
person-versus-delegated answer receipts. Standing schema 3 prevents older readers
from interpreting scoped rules as project rules; organization schema 3 adds only
empty governing bindings on upgrade. The prior shutdown/restart requirement still
applies. Chat collection tools expose place/unplace/governing; background agents
can inspect but cannot broaden those bindings.

Read-only governing and informational context now follow execution ownership
through child configurations. `context_trace` reads selected-input receipts and
original journal evidence; missing parent causal links and ambiguous overlaps
remain explicit. It does not claim complete causality, instruction compliance or
external-effect verification. All builds/tests in this wave run on Spark, including
targeted checks; no local compilation or tui3 tests are authorized.

The local-files wave adds the terminal door onto standing work and the causes
behind each firing. `--report <path>` names one workspace file the runner
publishes a task firing's final answer to, with a sha256 receipt in the run's
`occurrence.json`; the firing gets no wider write approval, a run that stops on a
question or is cut off leaves the previous report, and a report inside its own
watch is refused. Terminal-made orders deliver their notes to the project inbox.
The declining answer on a standing card no longer carries an adoption receipt.
A live run quoted an address a folder rule forbade even though the journal showed
the rule reached it, so a published report is now checked against the rules that
reached its run (auditor role, fresh context, no tools; a finding must quote the
report), corrected once, or held back; the check is recorded as `ruleCheck` in
`occurrence.json`. It covers the published report only — not tool actions, notes
or work without a report. Review fixes in the same wave: occurrence keys carry
the run count (a folder returning to an earlier reading no longer recovers an old
record forever), a report-only edit is an edit, a report in a watched folder is
refused, a published-but-unrecorded run is recovered rather than rerun, report
folders are contained before they are made, and a failed or held run's changes
are listed again. The live reports opened with the model's own narration, so a
run now writes its report between a `<report>` line and a `</report>` line and only
that is published (an answer without them is still published whole); a refused
attempt to write its own report file no longer leaves it waiting on the person, but
without a closed report it publishes nothing. Whether a run may publish is one
decision read from everything its turns came to (`standing_publish.go`), so a limit,
an unclosed report, a cut-off and a self-write each withhold it with their own line.
A second review found two more ways past it, both closed inside that decision: an
empty closed report, and an answer cut at the output limit after its continuations
ran out, which the run now reads off the turn's own ending rather than a flag beside
it. Every withheld run records its reason as a pinned code beside its outcome.
A third round kept each report together with the ending of the turn that wrote it,
held no-report orders to the same truthful outcomes, rechecked the stop and the pass
at the moment of each of aforge's own acts, and made the report write a compare-and-swap
against the last receipt, so a person's edit is never written over. The same round
applied the scale audit's consume-after-commit law to the inbox and answers drains,
repaired a journal's torn tail on open, and numbered run folders from a recorded
counter.
The fourth wave closes the live validation's failures (S02, S09c, S11, S12b, S17b,
S25a, S27b): the belt of an unattended run is what its rules grant, its reads are
confined to its project, the rules check answers per rule and names its placements,
a watch reads nested folders within a declared limit and ignores identical rewrites,
and a folder shows the work placed in it. `docs/design/workspace-foundation/grooming/BUILD-WAVE-04.md`
has each change with its regression, the old-logic proof and the live tables.
`make test-local-work` drives `bin/aforge` with a scripted
model and `make demo-local-work` runs the same journey with a real model in a
disposable `AFORGE_HOME`, failing on the first step that does not hold. No
connector or account is used.
