---
kind: changed
title: launches offer a new codeaf in a quieter, non-blocking way, and installs run in the background
pr: 1789
surface: [chat]
invalidates:
  - "`/update` used to refuse while a turn or task was running and then quit the program so the door could restart on the new build. It now installs in the background on the same road the automatic updater uses: nothing quits, nothing restarts, and a running turn or task is untouched. The new build is what opens the next time codeaf is started."
  - "The launch notice used to be a single dim line whose only action was `/update`. With `update.auto` on (the default) a newer release now raises an offer in the keys line, counted down live from ten seconds, and then installs itself in the background; `alt+n` or `/update skip` skips that exact release, and `/update never` or the settings row turns the automatic road off."
  - "A version behind its channel with `update.auto` off still gets one quiet line naming the release and the hand-run road, instead of no check at all."
  - "A codeaf owned by Homebrew, Nix or a system package manager, or one in a folder the account cannot write, is refused in place by both the chat and `codeaf update`, and the refusal names the manager and the curl road."
  - "The manuals `internal/manual/chat/staying-on-that-machine.md` and `running-on-another-machine.md` said a busy engine from an older build was replaced the moment a newer codeaf opened. They now say the truth the engine lane implements: an older engine holding work is joined and keeps it, and steps aside once it is quiet (or at the next safe launch for a host too old to know the ask); one holding nothing hands over at once; an attached idle window counts as holding work; and another wire, or one too old to be asked, is refused with `codeaf engine --stop --workspace '<path>'` named."
  - "One install now runs at a time per executable, across terminals and profiles: the lock is an OS advisory lock keyed on the executable AND IS TAKEN INSIDE the shared installer, so the chat surface and `codeaf update` are one acquirer. Under it a sha256-tied record says what is on disk: a duplicate is a quiet no-op and a stale automatic downgrade is refused, while an explicitly named tag may still roll back."
---

The offer waits in the line that already carries the idle keys, so it never
takes the message box, never blocks a keystroke and never binds a plain letter.
While its grace is open it stays visible even over a running turn's own keys by
appending a defer clause; when it expires the download begins in the background.
The sentence names the release, says it installs shortly, and carries the skip
chord and `/update` — both survive an eighty-column window, because the
automatic clause is dropped before either action is.

The completion wording is exact and quiet: `codeaf <tag> installed · this
session keeps running`, with the durable note adding that new windows use the
update while existing engines finish their work first and an attached window
keeps its engine current. It does not promise that a busy engine swaps. A
same-release candidate is a quiet successful no-op rather than an alarming
failure, and only a NAMED tag (`/update <tag>`, `update --version <tag>`) may
deliberately roll back; an automatic or channel update never does.

`update.auto`, `auto update` in `/settings`, is on by default. The dismissal and
the failure count live in one small file beside `config.json`, keyed by release
and by executable, so the same release never asks twice and a release that failed
three times stops being tried on its own.
