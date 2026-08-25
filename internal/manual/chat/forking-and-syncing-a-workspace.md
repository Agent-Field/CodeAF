# Forking and syncing a workspace

## Is furrow required, and what happens if I do not have it

**furrow is a separate program you install yourself, and aforge does not need it.**
Nothing on this page exists on a machine that does not have it — and that is by
design, not a gap. With no furrow installed, `workspace_snapshots`,
`workspace_restore`, `workspace_fork` and `workspace_merge` are **not on the tool
list at all**. They are not present and failing; the verbs simply do not exist, so
aforge will tell you it cannot put files back rather than trying and refusing.

furrow is open source, Apache-2.0, from Agent-Field:
**https://github.com/Agent-Field/furrow**. It copy-on-write forks a whole
workspace — every file, dependency, `.env`, the dev database, git's own mutable
state — into a byte-exact copy in about a second, and continuously seals that
workspace into an immutable timeline you can put back.

There are **two halves** and both have to be true:

1. **furrow is on this machine.** Install it yourself.
2. **This folder is attached to it.** Run `furrow watch` once, in the folder:

   ```
   cd my-project && furrow watch
   ```

Install furrow but never run `furrow watch` here and the four tools are still
absent — furrow itself says *"this repository is not watched; run `furrow watch`
first"*, and aforge takes that as the answer.

If furrow is installed somewhere a spawned program would not find it, set
`AFORGE_FURROW` to the full path of the binary. A value that names nothing is an
error rather than a quiet fall back to whatever is on `PATH`.

Nothing about furrow is switched on for you, nothing is uploaded anywhere, and
aforge never runs `furrow watch` on a folder by itself.

## Undo what the agent did to my files — restoring the workspace

**A rewind of the conversation never touches your files.** That is stated on the
sessions and rewind page and it stays true: rewind takes back what was *said*, and
a file the dropped turn wrote stays written.

Putting **files** back is a different thing, and it exists only with furrow
installed and this folder attached (see "Is furrow required"). Then:

- `workspace_snapshots` lists the restore points — moments the whole folder was
  sealed, newest first, each with an id, when it was sealed and what it was
  called. It reads only.
- `workspace_restore` puts the folder back to one of them.

`workspace_restore` **previews first**. Asked without confirmation it lists every
path it would touch, changes nothing, and says:

> Nothing applied — this is a preview. Call workspace_restore again with confirm
> true to apply it.

Confirmed, it restores and reports what it did:

> Restored 2 path(s) from aaaabbbbcccc0000.
> The state before this restore was sealed as ffff000011112222, so this restore
> can itself be undone.

That second line is the important one: **furrow seals the current state before it
restores**, so a restore is itself undoable. If there is nothing to do you get:

> Nothing to restore: the workspace already matches that restore point.

aforge will not restore on its own judgement. It previews, shows you the paths,
and waits for you to say yes.

## Restore .env, my dev database, or files git never saw

This is the case furrow exists for, and git cannot help with it. `git clean -fdx`,
a bad script, or an agent tidying too enthusiastically takes out `.env`, the local
SQLite database, ignored build state, and git's own index — none of which git is
protecting. furrow's timeline holds all of it.

With furrow installed and `furrow watch` run in this folder (see "Is furrow
required"), ask for one path back rather than the whole folder:

- `workspace_snapshots` to find a restore point from before it broke.
- `workspace_restore` with that id and `paths` set to `.env` — **newer work in
  every other file is left exactly alone.**

Restoration covers symlinks, permissions, extended attributes, SQLite databases
and git's mutable state. Every restore point declares how exactly it can be put
back; aforge shows that declaration beside the id, as furrow words it, rather than
promising something furrow did not.

Without furrow, none of this exists: no restore points, no `.env` back. aforge
will say so instead of pretending.

## Try something risky without breaking my project

With furrow installed and this folder attached (see "Is furrow required"),
`workspace_fork` runs a command inside a **copy-on-write fork of the entire
workspace** — files, dependencies, `.env`, the dev database — ready in about a
second. A dependency upgrade, a destructive migration, a wide refactor, a script
nobody trusts: it runs over there, and the real folder is not modified whatever
it does.

What comes back names the fork, says how the command ended, and says plainly that
your folder is untouched:

> Ran in fork risky; the command exited 1.
> The real workspace was not modified. Land it with workspace_merge on "risky".

Nothing lands until you ask. `workspace_merge` takes the fork's name and, ideally,
a **check** — your tests, a build. furrow assembles the merged result somewhere
else, runs the check there, and lands nothing unless it passes:

> The check failed, so nothing was merged.

Paths changed on both sides stop it too, and nothing lands:

> Nothing was merged: 1 path(s) changed on both sides.

Set `preview` to see the shape of a merge without doing it:

> Nothing merged — this is a preview. 4 path(s) would change, with no conflicts.

A successful merge says what it changed and what it sealed:

> Merged risky: 4 path(s) changed.
> Sealed as aaaabbbbcccc00ff.

## Parallel agents — several sessions in the same project at once

furrow's own answer to running more than one agent in a project is to give each
one its own **universe**: a full copy-on-write fork of the working state, so ten
of them cost roughly the disk of one and none of them fight over files, ports or
the dev database. In aforge that is `workspace_fork`, one fork per risky run or
per session, landed back with `workspace_merge` and its check.

Two things are worth knowing before you rely on it:

- **Nothing lands by itself.** Each fork is sealed and sits there until somebody
  merges it. A fork whose merge finds the same paths changed on both sides stops,
  and says so, and changes nothing.
- **This is furrow's machinery, not aforge's.** aforge does not orchestrate the
  agents, watch them for conflicts, or decide the merge order. `furrow forks`
  and `furrow ui`, run yourself, are where you see every universe, its real disk
  cost and its live conflicts.

Without furrow installed, two aforge sessions in one folder share that folder
exactly as they always have — see "Two terminals in the same folder" on the
sessions and rewind page. Nothing about that changes, and no fork is available.

## Sync my folder to the other machine — my laptop's files over there

If you want a folder that lives on your laptop to exist on the machine aforge is
running on, **aforge does not have its own file-sync**, and does not try to grow
one. What it does is offer furrow's pairing, when furrow is installed on both
machines.

Run these yourself, in the folder, on the machine that has it:

```
furrow remote add ssh://dev@machine-a.tailnet --name my-project
furrow sync --follow
```

and on the machine that should receive it:

```
FURROW_RECOVERY_KEY=<key> furrow clone ssh://dev@machine-a.tailnet/my-project
```

The clone is your **complete working state** — dirty edits, `.env`, dev database,
git index — and not a checkout of your last commit. Remotes hold only ciphertext;
the recovery key, entered once per machine, is the only thing that can read it.
The remote can be an SSH host over a LAN or a tailnet, or any S3-compatible
bucket used as a mailbox. No hosted service is involved.

**aforge never runs `furrow remote add` for you.** Pairing prints the recovery
key, and a key that passed through aforge would be written into a transcript. So
the commands are yours to run, and the key never reaches aforge at all.

One honest edge, furrow's own:

> Changes made on both machines at once are kept and reported, never merged for
> you — so let one machine be the one that writes.

## Work on my local project from the machine over there

The short answer: **work lives on the machine the engine is running on.** Over a
connection, files are read and written there, commands run there, and paths you
type mean paths over there — the running on another machine page says which
things are local and which are not.

If the project you actually want to work on is on your own machine, there are two
honest options:

1. **Copy it over and work there.** Simplest, and it is what most people mean.
2. **Pair the folder with furrow**, if furrow is installed on both machines — see
   "Sync my folder to the other machine". That gives the far machine your
   *current* state, not your last commit, and keeps it warm both ways with
   `furrow sync --follow`.

Option 2 is worth the setup only when you keep going back and forth. Note its
edge: cross-machine divergence is preserved and reported, never merged for you,
so let one machine be the one that writes — and over a connection, that is the
machine aforge is running on.

Without furrow installed, option 2 does not exist and aforge will say so rather
than offering a sync it does not have.

## When one of the workspace tools cannot do something

Every one of `workspace_snapshots`, `workspace_restore`, `workspace_fork` and
`workspace_merge` fails in furrow's words rather than in invented ones, because
furrow's words are what you will act on:

> furrow could not do that: this repository is not watched; run `furrow watch` first

If furrow answers something aforge cannot make sense of — a version whose output
changed shape — it says so plainly instead of guessing:

> furrow answered in a shape aforge does not understand

Nothing is retried, nothing is half-applied, and a restore that furrow did not
confirm is never reported as done.

Two more limits worth knowing:

- **A command that failed inside `workspace_fork` is not an error.** The fork was
  made, the command ran, and it said no — which is usually why you asked for a
  fork. The exit code and the output come back for you to read.
- **Long output is cut.** A fork's output and a merge check's output are capped,
  and the cut says how much was left behind: `… 5120 more bytes not shown`.

## What aforge does not do with furrow

Plainly, so you do not find out the hard way:

- **It does not install furrow, and it does not attach a folder.** `furrow watch`
  is yours to run. A folder aforge was never told to attach stays unattached.
- **It does not pair machines.** `furrow remote add`, `furrow sync` and
  `furrow clone` are yours to run, because pairing prints the recovery key.
- **It does not restore anything without your explicit yes.** `workspace_restore`
  previews unless you confirm it.
- **It does not merge without you asking.** A fork sits there until
  `workspace_merge` is called on it by name.
- **It does not read furrow's own dashboard.** `furrow forks`, `furrow timeline`,
  `furrow ui`, `furrow bisect`, `furrow try` and `furrow shrink` are furrow's
  commands, run in your own terminal. aforge uses four of furrow's verbs and no
  more.
- **It cannot tell you whether a remote is already paired.** furrow does not
  report that in a form aforge reads, so the sync commands are offered as
  something to run, not as a state to check.
