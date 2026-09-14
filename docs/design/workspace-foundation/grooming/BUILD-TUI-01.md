# TUI checkpoint 1 — browse, inspect, open, return

**Status: usable demonstration built and exercised on Spark; not a merge acceptance.**
Source `36922486c` on `codex/personal-experience-0914` (base `730182f51`): implementation
`9d7588a1e` plus its independent-review fixes. Implemented
by Claude Code Opus in Fleet job `20260914-192937-000446`, host `spark`, 2026-09-14 UTC.
The W5-B combined journey stays **not accepted** (21/37, BUILD-WAVE-05B); nothing here
changes that record.

## What a person can do now

`alt+8` (or `tab`, or a click on the bar's `folders`) opens an eighth place after
settings. It lists the folders no other folder files; `enter` goes in, `←`/`backspace`
walks back out along the path walked (`folders / Startup / Product`). Each record is one
row — `folder`, `chat`, `work`, `ongoing work`, `artifact` — with its state, `missing`
when its owner no longer has it, and `placed` when this folder's rules reach it.

From 88 columns a reserved inspector on the right reads the selected row; the list does
not move when the selection changes. Below 88, `→ d details` reads the same facts across
the page (`esc back to the list`). `enter` opens the row through its owner: the one
existing conversation (brought forward if held, never cloned), a finite task's record
on tasks, an ongoing item on standing, a file's preview. Leaving and returning keeps the
folder and the row. Words typed on the place start an ordinary chat in no folder. The
place is read-only: no filing, placing, pausing or editing controls are shown.

## Change → effect → evidence

| Change | Effect | Evidence |
| --- | --- | --- |
| `workspace.Store.TopLevel`; `workspaceview.Folder` joins memberships (seq order) and governing placements (cursor-windowed) into one row per record with `Filed`/`Placed` flags; `limit` → `More` | A record both filed and placed is drawn once; filing never becomes placement; a missing record keeps its row | `folder_test.go`: `TestAFolderPageJoinsFiledAndPlacedWithoutDuplicatesOrInferredAuthority`, `…TopLevelIsFoldersNoMembershipContains`, `…SaysWhatIsMissingRatherThanHidingIt`, `…StopsAtItsLimit…` |
| `workspaceview.Item`: filed in, placed in (governing), context, standing item, rules reaching it via the shared `RulesReaching` (also now used by `aforge standing show`); each section fails independently into `Errors` | The inspector shows what the run itself would read; a failed section says `could not be read: …` | `TestAnItemSaysWhereItIsFiledPlacedAndWhichRulesReachIt` |
| `workspaceview.Artifact`: only a path some folder files or places; not a directory; 64 KiB head; NUL/invalid UTF-8 → binary | Preview cannot read an arbitrary path on the engine machine | `TestAnArtifactPreviewReadsOnlyWhatAFolderNames` |
| `remote` `Collections.Page|Item|File` (read-only; no protocol version change); engine, in-process and `--host` roads all read the serving engine's `collections.db` | No laptop store stands in for a remote engine; an older engine's `engine: no such method "Collections.Page"` is drawn as `could not read this folder · …`, an engine built without the readings answers `this engine cannot read its folders`, and a surface with no seam says `Folders cannot be read here…` — never an empty folder | `collections_test.go` `TestTheFoldersCrossTheWireWholeAndAnAbsentEngineRefuses`; wiring in `engine.go`, `chatv3.go`, `chatv3_host.go` |
| tui3 `place_folders.go`, `foldersplace.go`: async page/item/file reads with a 15 s bound, generation + id checks, 120 ms debounce on the item read; path and chosen row kept across `close` | Stale answers are never drawn; a failed reading keeps rows and says why | `place_folders_test.go` (10 tests incl. `TestAStaleFolderAnswerIsNeverDrawn`, `TestAFailedOrAbsentReadingIsSaidAndNeverAnEmptyFolder`, `TestTheSharedChatOpensAsOneConversationFromEitherFolder`) |
| Places bar: a bare rung (unbanded words lose padding) before the `▸ N more` fold; count words 7 → 8 | All eight words fit at 60 columns | `narrow_test.go`, `pages_test.go` `TestThePlaceCountWordsAreTheBar`; screen `tui01-screen-17-narrow.txt` |
| `aforge-demo-home --personal` + `scripts/demo-personal.sh` | Fixture profile through supported doors; only history (3 conversations, 1 task) is written directly and says `(fixture)`; no model, no timer, `AFORGE_HOME` only | `TestThePersonalFixtureIsReadBackFromItsOwnStateRoot`; [fixture log](validation/tui01-fixture-stable.log) |
| Manual `places.md` (new `## folders …`), `collections.md` and seven→eight across pages; four probes; #662 change entry | The chat can answer how to browse folders and what `filed here` means | `go test ./internal/manual/` and `make test-packed-manual` green |

## Actual terminal acceptance

Driver [tui01-accept.sh.txt](validation/tui01-accept.sh.txt) launched the pinned
`bin/aforge` through `ssh -tt spark '…'` in tmux (140×40, then 60×30) against a copy
profile seeded by the same script (`/tmp/af-pai-accept-36922486c`). The run at
`36922486c` ended 2026-09-14T20:28:55Z: **48 PASS, 0 FAIL** ([checks](validation/tui01-accept-checks.txt)).
The same driver also passed 48/48 at `9d7588a1e` (20:15:54Z, `/tmp/af-pai-exp-logs/accept2/`); its
first run there had two driver-check errors (a column probe matched the inspector heading; the
conversation tab row is line 2 over ssh), fixed in the driver, not the product.

Keys and mouse used: `alt+8`, `enter`, `↓`×6, SGR clicks on rows 9, 10 and 7, `enter`,
`alt+8`, `←`, `↓ enter`, `enter`, `alt+8`, `← ↑ enter ↓↓ enter`, `alt+8 ↓×4 enter`,
resize 60×30, `→`, `d`, `esc`, resize 140×40, `alt+1`, click on the bar's `folders`, `alt+1`,
click + `enter` on the unfiled chat.

| Journey step | Receipt |
| --- | --- |
| Enter folders, walk Startup → Product | [product](validation/tui01-screen-04-product.txt): filed chats, `work · done`, two artifacts, `chat · missing`, `ongoing work · active · placed` |
| Inspector changes, list stays | [ongoing](validation/tui01-screen-05-ongoing.txt) → click task → [spec preview](validation/tui01-screen-07-click-spec.txt); `spec.md` list column 4 in all three |
| Same chat from Product, then Marketing | both open the fixture transcript; one `Pricing and` tab; session directories identical before/after; transcript still 8 lines; meta workspace unchanged (`…/fixture/startup`) |
| Return keeps path and selection | [Product](validation/tui01-screen-09-return-product.txt), [Marketing](validation/tui01-screen-13-return-marketing.txt): crumb and highlighted row (`48;5;237`) preserved |
| Work via its owner | finite task → tasks record (`Fixture record: a first draft…`); ongoing → standing place at the item |
| Artifact preview | `- Export: CSV and PDF.` read through the engine's `Collections.File` |
| Narrow | [60×30 list](validation/tui01-screen-17-narrow.txt): all eight bar words, `ongoing work · placed` (state given up before `placed`), `→ verbs` in the foot, no inspector; [details page](validation/tui01-screen-19-narrow-detail.txt) |
| Direct chat | home still lists `Quick Question About Tar Flags`; opening it shows the fixture turn |

Owner identities ([receipts](validation/tui01-owner-receipts.json), `aforge collections find`):
the shared chat is referenced by Product and Marketing and placed nowhere; the ongoing
item is placed in Product and filed nowhere; the task is filed in Product; the unfiled
chat is in nothing.

Logs outside the repository: `/tmp/af-pai-exp-logs/` (`tui3-focus2.log`, `tui3-focus3.log`,
`laws1.log`, `narrow-pkgs1.log`, `review-fix1.log`–`review-fix3.log`, `accept1/`, `accept2/`, `accept2-run1/`, `accept3/`, `launch-stable-36922486c/`).
Build: [tui01-build.log](validation/tui01-build.log) (`aforge 36922486c built 2026-09-14 16:27` local time, 20:27Z).

## Independent review and fixes

A read-only Opus review of `9d7588a1e` found one blocker and three should-fix items;
all were fixed in `36922486c`:

| Finding | Fix | Evidence |
| --- | --- | --- |
| BLOCKER: a filed path naming a FIFO blocked `os.Open` forever (stalling every later call on an engine connection); `/dev/stdin` on an engine is the wire | Regular files only, checked by `Stat` before and `Fstat` after an `O_NONBLOCK` open; each `Collections.*` call bounded to 15 s on the engine side | `TestAnArtifactPreviewRefusesANamedPipeWithoutWaiting` (unix) |
| Every 3 s beat bumped the generation, so any answer slower than a beat was dropped forever; Item + 64 KiB File re-sent each beat | No beat read while one is out; same-row refresh re-reads owner facts only; `reading this folder…` until first answer; world read on open/enter only | `TestABeatDoesNotOutrunAFolderReadingThatIsStillOut` |
| Bare rung padded the bar cursor's word, so words moved as the cursor walked; dead cells between bare words | Only the current place is padded; an unpadded word owns the space before it | `TestTheBareBarHoldsStillWhileItsCursorWalks` |
| Preview re-split and re-highlighted per frame | Drawn rows cached by path, change time and width | focused tui3 set |
| Nits | `More` only for real unfiled overflow; hover reset; inspector paths sanitized; foot names `go in`/`preview`; misattached doc comments and stale seven-place comments; older-engine wording quoted exactly; manual bar arithmetic; demo script parses ids as JSON | laws, manual, packed manual, `GOOS=windows go vet` |

Not adopted: renaming `foldersplace.go` (kept to avoid churn in this checkpoint).

## Stable demonstration

- Worktree: `/home/santosh/src/af-pai-demo-36922486c` (detached at `36922486c`, clean, `make build`).
- Profile: `/home/santosh/aforge-pai-demo-36922486c` (config: memory off, background checks off; no key stored).
- Launch ([verified](validation/tui01-launch-command.txt)): the key is read at launch from the person's existing Spark profile and never printed or stored.

```sh
ssh -tt spark 'cd /home/santosh/aforge-pai-demo-36922486c/fixture/startup && AFORGE_HOME=/home/santosh/aforge-pai-demo-36922486c OPENROUTER_API_KEY="$(jq -r .api_key ~/.aforge/config.json)" /home/santosh/src/af-pai-demo-36922486c/bin/aforge'
```

Browsing calls no model. Sending a message in any chat is an ordinary paid turn. Without
the key variable the first-run setup appears; `esc` skips it and browsing still works.

## Checks run, and not run

Run on Spark (`GOMAXPROCS=4 GOFLAGS=-p=2`): focused tui3 set (folders, places, bar,
chords, help, narrow, hint, manual, icon) green at both revisions; `internal/workspace`, `workspaceview`, `remote`,
`session` (the whole package ran, 228 s, green), `manual`, untagged `e2e`, `ci`;
`make test-laws`; `make test-packed-manual`; `go vet` of the touched packages;
`TestThePersonalFixture…`.

Pre-existing reds seen, not caused here: `TestTheOpeningHintNamesBothDoors` fails the
same at `730182f51` (temporary worktree, removed). Nine `cmd/aforge-demo-home` tests
fail in `seedDemoHome` (`deps-weekly` refused for `{{evidence}}` on an every item); the
seeder's standing files are untouched by this change, so this is inferred pre-existing,
not rerun at base. Neither is in `.github/known-red.txt`.

Not run: the full tui3 package, full `cmd/aforge`, the tagged E2E package, `make test-remote`.

## Limitations

- `esc` on a task record lands on the tasks place; `alt+8` returns to the kept folder and row.
- The home switcher still opens on the most recent conversation in the launch directory.
- Folder rows say nothing about their contents' activity; a folder's inspector names rules and parents only.
- A long path wraps inside the inspector's `where`/`project` values.
- Refresh is the place clock's beat (skipped while a reading is out); no pagination beyond 500 rows (`more are in this folder than one page shows`).
- The inspector's `spoke`/`in chat` facts come from the world read when the place opened.
- Artifact identity is an absolute path; a moved file becomes `missing`.
- Controls (file, place, pause, resume, stop, edit) are deliberately absent.

## Checkpoint 2 — proposed next bounded slice

Continuing work in the same fixture, through the owners that already exist:
1. **Work inspector completeness:** last run and its report with receipt time, next
   expected check honestly (`checked only while a window is open, or on aforge standing
   check`), cause of the last run, instructions version.
2. **Expose existing controls, bounded:** `p` pause/resume and `s` stop on an ongoing
   row, reusing the standing place's own verbs and confirmations and the revision-aware
   owner operations; nothing is offered where the owner has no operation. Instruction and
   report-path edits stay on the chat card (W5-B's G2 is not reliable: live 5 edited
   instructions instead of `does.report`), and the inspector says so rather than
   promising a rename.
3. **Observe a change:** edit a fixture file, `aforge standing check` (explicit, paid,
   bounded), and see state/report/cause update in the inspector without the cursor jumping.
4. Acceptance in the same tmux driver shape, with a spend cap and receipts; W5-B stays open.
