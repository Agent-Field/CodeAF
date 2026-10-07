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

- `/update` (or `/upgrade`) — install now; an explicit channel or tag wins, and
  a NAMED tag is the one deliberate rollback.
- `alt+n`, or `/update skip` — skip this release. It is written down and that
  exact tag is never offered again.
- `/update never`, or the `auto update` row in `/settings` — stop installing
  automatically. The launch check still runs and still says what is out.

The three are the ONLY commands the offer prints, and a test extracts every
`/update` token from the hint and the note and checks the parser answers each as
itself rather than as a tag. While the grace is open the offer stays reachable
even over a running turn's own keys, because `footHint` appends a defer clause to
whatever outranks it; `hintShorter` drops that clause first, so a narrow frame
loses the offer and never the turn's keys. A question page owns the frame, so the
grace PAUSES under one instead of counting out invisibly.

## What a background install does and says

`/update` typed by hand takes the same road as the automatic one. Both download,
verify sha256, and replace the executable atomically while this process keeps
running the build it started on. Neither quits, neither restarts a conversation,
and neither stops a turn or a task. The completion sentences are:

```
checksum matched · installed at <path>          (hand-run only)
codeaf <tag> installed · this session keeps running
```

The durable note adds: `new windows use the update; existing engines finish their
work first, and an attached window keeps its engine current`. That is the engine
lane's contract, stated without over-promising: a file replaced on disk does not
force a running engine to hand over, and an idle line never claims it did.

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
resolved target and not on a profile: two profiles can share one codeaf file, and
it is the file that must not be replaced twice at once. A process that dies
releases the lock in the kernel, so nothing is ever taken over by guessing at an
age.

THE LOCK IS TAKEN INSIDE `codeupdate.Install` AND NOWHERE ELSE, so the chat
surface and `codeaf update` are one acquirer with nothing to keep in step. Under
the lock the installer reads the record, which is trusted only while the file
still hashes to the tag — a binary replaced by curl, a package manager or a
person reads as unknown:

- a candidate already on disk is a quiet successful no-op (`Already`);
- a candidate older than what is on disk is refused UNLESS the person named that
  exact tag, which is a deliberate rollback (`AllowDowngrade`);
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
them at 80 and 120 columns with the real binary.

These are deterministic render fixtures, labelled in the frame itself, and they
clear every updater seam so a capture machine needs no network and cannot install
anything. They are not evidence that a background install preserves a real turn;
that continuity is REQUIRED VALIDATION to be recorded from the E2E suite before
this is treated as proved — a fixture shows the surface, never the continuity.
