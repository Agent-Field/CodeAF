---
kind: fixed
title: a plain launch sits down in the conversation the engine already holds
pr: 736
surface: [chat, engine]
invalidates:
  - "The engine host filed a conversation opened by a hello that named no session under the empty string. It no longer does: which conversation \"nothing named\" means is resolved to a TRANSCRIPT PATH at the door (cmd/aforge's engineHelloKey, reading cmd/aforge's v3LatestTranscript — the same resume order the boot applies), and nothing is ever keyed by \"\"."
  - "A host refusal used to push the window onto the in-process road. It no longer does: when the engine answers that the conversation asked for is held elsewhere, the launch stays on the engine road, opens a conversation of its own through the engine (Hello.New) and lands on home with the held row armed — so tui3.Options.EngineAnswers is wired and enter on that row moves the conversation at once. The genuine in-process fallbacks (unreachable host, wrong wire version, onboarding, --once, --debug, --no-host) are unchanged."
  - "The sentence \"this conversation is open in another window — … aforge engine --stop --workspace X\" named session.Config.Workspace, which for an OWNED conversation is that session's own work/ folder under ~/.aforge/v3/projects rather than a directory a host is keyed by. It names the project now; v3Launch carries it as Project."
---

A plain `aforge` in a workspace whose engine already held conversations could be
refused by that engine, about a journal the engine itself had the flock on. The
key a nameless hello resolved to was the empty string, which worked exactly as
long as that slot held the workspace's latest; once its conversation ended — moved
to another window, left behind by `/new`, closed — while the host went on holding
another, the next plain launch found nothing under `""` and booted, and the boot
resolved this workspace's latest, which is the journal the host was holding. The
client then fell back in-process with a brand-new conversation, where there is no
engine to ask, so `enter` on the held row took the road a daemon never answers.
