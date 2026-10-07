# Background updates and the launch offer

*The contract for how codeaf keeps itself current: one coordinator, one offer,
one installer per executable. `internal/update` is the machinery, `internal/tui3`
draws it, and this file says why the shape is what it is.*

## What a person sees

On launch, behind the newest release of its own channel, a build with `update.auto`
on raises one sentence in the keys line:

```
codeaf v0.9.3 is out · installs in 10s · alt+n skip · /update
```

It is not modal, it does not take the message box, and it binds no plain letter.
The count is live — the line repaints once a second from the deadline — so it can
never show a frozen number. After [codeupdate.AutoGrace] the download begins by
itself. The three answers are:

- `/update` (or `/upgrade`) — install now. Typed bare while the offer is up it
  installs the release the offer is about, and it is NOT a rollback: the tag is
  the surface's, not the person's. An explicit channel or tag wins, and only a
  NAMED tag is the deliberate rollback.
- `alt+n`, or `/update skip` — skip this release. It is written down for that
  exact tag and that tag is never offered again; a newer release is a new
  question. If the write itself fails, the note says so instead of claiming a
  skip that was not saved — this window still stops offering it.
- `/update never`, or the `auto update` row on `/settings`' **Workspace** tab
  (beside `background checks`) — stop installing
  automatically. It does not cancel an install already downloading and does not
  say it did. The launch check still runs and still says what is out: with the
  row off the one request is still made and the release is named on one dim
  line. Only `CODEAF_NO_UPDATE_CHECK=1` takes the look away.

The three are the ONLY commands the offer prints, and a test extracts every
`/update` token from the hint and the note and checks the parser answers each as
itself rather than as a tag. While the grace is open the offer stays reachable
even over a running turn's own keys, because `footHint` appends a defer clause to
whatever outranks it; `hintShorter` shortens the TURN's own clauses first and
keeps the offer's, so a narrow frame under a countdown never loses the one
control that countdown has — the offer's clause goes only when the turn is down
to its last key.

The clock PAUSES wherever the offer cannot be read or answered: a question page,
every place but home and the conversation (whose keys row is the offer's own —
`/settings`, memory, standing, spend), the first-run sheet, a team's card, the
move picker, the switcher's menu, the wall, the provider panel, and the settings
panel's own value box and model picker. The list is `[app.key]`'s rung order
above the offer's chord, and a test pins its scope in both directions: a running
turn and a plain conversation MUST keep counting.

The row is read LIVE. `/settings` updates the running window's flag, and a row
turned off — or a release skipped — in ANOTHER window is seen because the
coordinator re-reads the profile on every countdown beat, at the deadline, and
again after the resolver answers and before anything is downloaded. A stale
ten-second-old decision never installs over a newer one.

## What a background install does and says

`/update` typed by hand takes the same road as the automatic one. Both download,
verify sha256, and replace the executable atomically while this process keeps
running the build it started on. Neither quits, neither restarts a conversation,
and neither stops a turn or a task. The completion sentences are:

```
checksum matched · installed at <path>          (hand-run only)
codeaf <tag> installed · this session keeps running
```

The durable note says what is true without the engine's internal vocabulary: `new
windows open the updated app; work already running keeps its current engine until
all its windows and work close`. A file replaced on disk does not force a
running engine to hand over, so the line never claims it did, and it no longer
leaves "current" to be guessed at.

A finished line dwells for [updateOfferingDwell] and then folds; the transcript
note is the durable record. A same-release candidate is a quiet successful no-op:
`codeaf <tag> is already the build on disk · this session is untouched`.

There is no restart-on-the-spot action. A new build runs at the next launch.

## The coordinator's memory

One small file in the profile, `update-state.json`:

- the tag a person dismissed, with the moment, so a newer tag is a new question;
- the tag that last failed, how many times, and when, which becomes a backoff
  (doubling from 6h) and stops after three failures.

`internal/config`'s `update.auto` row is the standing switch, default on. The
offer's own opt-out writes through the same writer the settings row does.

## One installer per executable

The lock is an OS advisory lock on `<executable>.install.lock`, keyed on the
target and not on a profile: two profiles can share one codeaf file, and it is
the file that must not be replaced twice at once. A process that dies releases
the lock in the kernel, so nothing is ever taken over by guessing at an age.

THE TARGET IS CANONICALIZED BEFORE THE LOCK, THE REFUSAL OR THE TEMPORARY FILE
IS NAMED: absolute, cleaned, and resolved through symlinks (the directory too,
for a target that does not exist yet). A relative path, a `..`, or a symlinked
folder is therefore the SAME target as the real one, so holding an alias
serializes against an installer using the real path — proved by a cross-process
test and by a symlinked-folder test inside `internal/update`. The ownership
screen (`InstallRefusal`) canonicalizes the same way, so a package-managed file
reached through a symlink is still refused instead of answering "ordinary".

THE LOCK IS TAKEN INSIDE `codeupdate.Install` AND NOWHERE ELSE, so the chat
surface and `codeaf update` are one acquirer with nothing to keep in step. Under
the lock the installer reads the record, which is trusted only while the file
still hashes to the tag — a binary replaced by curl, a package manager or a
person reads as unknown:

- a candidate already on disk is a quiet successful no-op (`Already`);
- a candidate older than what is on disk is refused UNLESS the person named that
  exact tag, which is a deliberate rollback (`AllowDowngrade`). A channel, the
  automatic road, and the bare `/update` that answers an offer all follow the
  release line forward, so a file another window advanced is never stepped back;
- a NAMED tag reaches the installer even when it matches the PROCESS's own
  revision stamp, because the stamp is not the file on disk — another window may
  have advanced the file while this one kept working, and a person asking for
  that exact release is asking for that exact file;
- a target another process is mid-install on is refused with `another codeaf is
  already installing an update`.

Channel builds are ordered by the release's published moment, the same
`CompareChannelBuilds` the launch check uses, so two same-day dev builds are
ordered by fact rather than by guess.

## What it refuses

- A source or unstamped build: `this codeaf was built from source · rebuild with
  make build, or install a release: <curl>`.
- An unsupported platform: the installer's own error, recorded as a failure.
- A checksum that does not match, or a missing checksum: the installer's error;
  the original file is untouched because the download lands in a temporary file
  and only a verified body is moved into place.
- A file owned by Homebrew, Nix or a system package manager, or one in a folder
  this account cannot write: refused in place, with the manager and the curl road
  named, before anything is downloaded.

## Render fixtures

`CODEAF_UPDATE_DEMO` names one state so a capture can be taken without waiting
out a grace or paying for a download: `offer`, `downloading`, `ready`,
`ready-working`, `manual`, `off`, `failure`. Every case first clears the updater
seams, so a fixture cannot make a request or replace a file, and each says at the
top of the transcript that it is a fixture. `scripts/update-drive.sh` captures
them at 80 and 120 columns with the real binary, and captures the REAL `/settings`
page beside them (`settings-auto-on`, `settings-auto-off`): those two open the
settings place through `/settings`, walk the tabs to Workspace and the cursor onto
the row with the panel's own arrow keys, and the capture is only kept once the
row's OWN hint is the sentence on screen — so what is drawn is the registry's
row reading the profile's value, not whatever a fuzzy search happened to put
first. The off capture's profile is the only thing that differs, and it carries
the sheet's own changed marker. Every capture runs with an empty environment, an isolated
home, a private tmux server, `--no-host`, `CODEAF_NO_UPDATE_CHECK=1` and a
loopback `CODEAF_BASE_URL`, so no capture can reach a release host or a provider,
and the drive removes only the directory it made.

These are deterministic render fixtures, labelled in the frame itself, and they
clear every updater seam so a capture machine needs no network and cannot install
anything. They are not evidence that a background install preserves a real turn;
that continuity is REQUIRED VALIDATION to be recorded from the E2E suite before
this is treated as proved — a fixture shows the surface, never the continuity.

## Maintainer note: shared storage is part of the wire contract

The files this feature shares between windows and between builds —
`update-state.json`, the `<executable>.install.lock` and the install record it
carries — are read across codeaf versions, and the state root may live on a
machine a surface only reaches over the engine wire. A change that BREAKS their
storage contract (renaming or retyping a field, or changing what an existing one
means) MUST bump the engine wire compatibility version (`remote.Version`): the
door refuses a mismatch, and that refusal is the only honest answer when a newer
surface would misread an older engine's storage. Joining an older engine is safe
only WITHIN that compatibility contract — adding an optional field or reading
one the older build already wrote is fine; redefining what is there is not.
