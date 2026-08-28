# QA — the interaction lane, driven against the built binary

2026-08-25. The owner ran `bin/aforge` and reported six things: tab clicks dead, no wheel
scroll, no hover preview, "sessions and other things do not seem to be on", "left/right does
not move tabs only shift does", "scroll down does not work".

This is what each one turned out to be. Everything here was checked twice — once as a test
in `internal/tui3`, and once by driving the real binary in tmux at 80, 120, 160 and 200
columns against a throwaway `AFORGE_HOME`.

## The fixture the binary was driven against

`$AFORGE_HOME/v3/projects/<encoded-workspace>/<session-id>/` with five projects, sixteen
conversations (three of them put away), two of them alive with running tasks, a project
folder with nothing in it, and a task index per bucket. Three things about building one of
these are worth writing down, because two of them cost this lane an hour each:

- **The bucket directory name is the workspace path with every `/` turned into `-`.** A
  bucket named anything else is still read as a project — `readProject` takes the project's
  name from a session's `meta.json` — but the *current* session's task index is looked up by
  the encoded name, so the tasks place opens on nothing.
- **A transcript must be a real journal.** `{"type":"message","role":"user","content":…}`,
  not an invented shape. `v3EmptySession` asks `session.Peek`, and a file Peek cannot read as
  a conversation is an empty shell — see the hazard below.
- **Do not build one under `/tmp`.** `SweepPlaces` rule 2 reaps a temp-rooted session a week
  after anybody last spoke to it, which is correct behaviour and makes a fixture rot under
  you between two launches.

## The six symptoms

| Reported | What it was |
| --- | --- |
| tab clicks dead | Real. The bar was seven labels — the draw recorded no spans. Fixed in `fe5ad071`; the press is `placeTabPress`, and `7733b362` asks it of all seven places. |
| no wheel scroll | Real on the four promoted places, which answered no wheel at all. Fixed in `fe5ad071`; one turn is three rows on every place, pinned in `7733b362`. |
| no hover preview | Real on the four promoted places (the standing list's map answered `-1` by declaration; the other three had none). Fixed in `fe5ad071`. `a050761a` adds the pin the old tests could not give: the preview is driven by a real `tea.MouseMotionMsg` through `Update`, not by calling the handler. |
| "left/right does not move tabs only shift does" | `tab` could not leave home — it walked into the tasks place, which refused on a machine with no work, and was put straight back. `shift+tab` landed on settings, which always opens, so it alone appeared to work. Fixed in `fe5ad071` (`walkPage` asks `pageReady` before turning a handle). Neither plain arrow is a tab key and neither is `shift+←→` — those are time (SCREEN 3d) — and `f7770629` pins that on all seven. |
| "scroll down does not work" | The window did not follow the cursor on memory, spend and search: `↓` walked the cursor off the screen. Fixed in `fe5ad071`. `a050761a` sharpens the law — the far edge of the window has to advance, not merely "the cursor is still drawn". |
| "sessions and other things do not seem to be on" | **Not reproducible.** See below. |

## "Sessions do not seem to be on" — exactly what was found

Home lists every conversation on the machine, or folds it behind one line that names the
exact number. Driven against a machine with thirteen live conversations across four
projects, at 80, 120, 160 and 200 columns, home drew `13 chats · what wants you first`,
eight rows, and `▸ 5 more, quiet since aug 23` — 8 + 5 = 13 at every width. Opening the fold
drew all thirteen and turned the mark to `▾`.

The four things this lane was asked to audit:

- **`hideQuiet` is off by default.** It is `app.switchQuiet`, a plain bool seeded to its zero
  value and written only by `alt+q`. Nothing persists it, so it cannot be on at launch.
- **`switcherShown` is 8, and the cap is never silent.** The fold line carries the count of
  what it stands over and the date it goes quiet from, and `switcherReading.chatCount` — the
  number in the head line — counts every unarchived row whether drawn or not.
- **Archived conversations are deliberately out of the resting list** and are found by
  typing: `buildWorld` ranks a put-away row under a query and says so in its own comment.
  Verified live — typing `put away` surfaced the archived conversation of that name.
- **A bucket with no conversation in it is not a project.** `readProject` answers false for
  it, so an encoded directory a launch left behind draws no heading. The fixture's empty
  project folder is correctly absent.

**What actually made conversations disappear during this lane, and why it is worth knowing.**
The first fixture's transcripts were an invented shape. `session.Peek` read no messages in
them, `v3EmptySession` therefore called each folder an empty shell, and `v3ReapEmpty`
`rm -rf`'d four of them on the next launch — conversations with titles, timestamps, spend and
a task index, gone from the disk.

That is a hazard the sweep next door explicitly refuses to take: `sweep.go` rule 3 says a
session whose `meta.json` is missing or unreadable **stays**, because "hiding somebody's
conversation on the strength of a lookup file is the more expensive mistake". The launch
groom applies the opposite rule to the transcript itself — a first line it cannot parse, a
truncated file, a journal from a future schema, and the folder is removed with its `work/`
untouched only because `v3EmptySession` checked for one. No fix is attempted here: it is
`cmd/aforge`'s, and what the rule should be is the owner's call. Flagged, not fixed.

## One defect this lane did find, and fixed

**`alt+1`…`alt+7` did nothing at all in a conversation** — no place, no refusal, no sound.
`placeKey` is reached from the seven places' own key handlers and nowhere else, which is
right for five of its six classes and wrong for the jump: it is how a person GETS to a room.
The manual had promised it there the whole time ("`alt+1` goes straight there from anywhere",
`home.md`). `f7770629` gives that one class its own function and reads it on the
conversation's road, under every modal claim and over the composer.

## What the merge onto the places changed about two of these

This lane was rebased on `ecfd2520` and landed alongside the lane that made tasks and
standing into places. Two of the fixes above read differently on the merged tree, and the
tests were rewritten to the merged law rather than to the shape either side had alone
(`placewalk_test.go`).

**The tasks place stopped refusing.** `showTaskPlace` — the tab bar's door — opens it on
whatever the reading holds, and an empty one spends the frame on `tasksTeach` saying what
the place is for. So `pageReady` no longer names it: walking into a room is not asking it a
question. The COMMAND still refuses, because `/history` typed on purpose that answered with
silence would read as a command that broke. The two rooms `walkPage` still steps past are
**standing** (nothing stands anywhere on this computer) and **memory** (memory is off).

**`refusePage` is unchanged and still carries both halves**, and the standing place's
`standNothingWord` now goes through it: typed as `/standing` into a conversation it is a
note in that conversation, exactly as it always was, and pressed as `alt+3` while a place is
up it is the router's own line — the same sentence, put where it can be read.
