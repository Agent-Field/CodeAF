---
kind: removed
title: the chat edits the folder itself, and the copy it used to keep is gone
pr: 990
surface: [chat, engine]
invalidates:
  - "a folder the person attached got a WORKING COPY of its own — a git worktree off its HEAD, or a clonefile/copy of a plain folder — and the conversation's read, write and edit were aimed into it. There is no copy now, for any folder, ever. Every hand writes in the folder the person attached, the moment it is called. internal/session/standingtree.go, standingbelt.go, the StandingTree / StandingChange / FolderLanding types, Meta.trees, Agent.Land / LandingFor / UnlandedChanges and Agent.StandingTrees are all deleted."
  - "`/land` and `/land now` put the waiting work into a folder, and a dim row above the message box said `changes for <name> · 3 files · /land` while something was waiting. There is no /land command, no landing card, no waiting row and no folderLander door over --host; typing /land gets `there is no command called /land · / lists them`. internal/tui3/landcmd.go is deleted."
  - "the first write into an attached folder handed the model a sentence saying 'Your changes to X are being kept for this conversation and are not in X itself yet. Go on using the same paths — reading and writing them reaches your own version.' That sentence was false for bash, grep, find and ls, and it is gone with the copy. Nothing is said on a write now; the question is asked before it instead."
  - "`bash is NOT redirected and the manual says so` (109-places.md) was the stated exception. There is no redirection left to be an exception to: read, write, edit, bash, grep, find and ls all reach the same disk the person does, which is what the manual now says."
  - "the `# Attached folders` block told the model that an attached folder is a REFERENCE and that the workspace `is still what may be written to` — the opposite of what the belt did. It now says the working directory has not moved AND that writes under an attached path go into that folder itself, with nothing to land afterwards."
  - "nothing was asked before the conversation first changed a file in an attached folder, because the copy stood in for the question. The first call that could change something under an attached folder now raises the ordinary approval card naming that folder (`the first change in /Users/…, which is the folder itself`), and a standing yes there is remembered PER FOLDER for the conversation — covering bash in that folder, which a memo keyed by tool never could. It does not fire under `tools.approvalMode: allow` or `--yolo`, and it never lifts internal/approval's critical floor."
  - "`# Project` carried the workstation, the working directory and the clock, and a turn that needed its own branch opened with `bash git status`. It now carries, for the working directory and for every attached repository, the branch, how many files are not committed, and how far the branch stands from its upstream — read beside the work with offpath.Take and folded into message[0] at the start of a turn only, never waited on."
  - "saying `work in this folder directly` / `edit it in place` took the copy back and made writes direct. Writes were already direct, so the word no longer changes anything the conversation writes; PlaceRef.Mode still says how a TASK given that folder stands in it, and PlaceRef.staged / keptAside are gone."
---

The chat-side standing tree is deleted. It was cut for every attached folder, aimed by a
`path` argument, and therefore covered three of the seven hands on the belt: on the turn
that provoked the `simplify` wave, 27 of 62 calls were `bash` — `git pull`, `git checkout
-b`, `go build`, all in the person's real repository — while six edits went into the copy,
and the model spent the turn reasoning from a notice that told it otherwise. The copy was
taken at one commit and never refreshed, so `/land now` refused the moment the model's own
`git pull` moved the branch, and what it left behind was stray branches in somebody's
project.

A worktree isolates concurrent unattended writers. That is the TASK case, and tasks keep
theirs unchanged (`taskTree.comeHome`, `taskTree.landMirror`, and `treehold.go`'s refusal
when a running node holds a tree — which matters more now, not less). A chat is one person
watching one model, and git is the undo.

What replaces it is one question through the gate that already asks: `Agent.folderSays`
sits beside `Agent.capabilitySays` in `decide`, names the folder in the card, and banks a
standing yes against the FOLDER rather than the tool, so the next `bash` in it is covered.
Nothing is raised that the policy was not already going to ask about, so a session on
`allow` or `--yolo` is asked nothing.

And because the conversation now writes where the person can see it, `# Project` carries
what git already knows — branch, uncommitted count, ahead/behind — gathered with
`offpath.Take` beside the work and folded into message[0] at the start of a turn, never in
front of one.
