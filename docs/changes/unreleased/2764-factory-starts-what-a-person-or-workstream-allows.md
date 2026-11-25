---
kind: fixed
title: The factory starts only what a person or a workstream's rule allows, and says how to publish
pr: 2764
surface: [engine, factory, chat]
invalidates:
  - "Turning a repository on queued every open issue carrying a start label (its own factory.yaml's agent-ready, say) in the same minute, while the Repos page said nothing runs until someone starts them. repos.json now keeps when each repository was turned on (on_since), and an issue opened before that arrives held in Backlog (setup_held) whatever its labels. Only issues opened afterwards start by label."
  - "On an organization's server a workstream's Factory switch did nothing: rules.keeps_running.factory was read by no one on the server, a new issue joined no workstream, and its job ran under the repository's own budget ($5) rather than the workstream's Start without asking. The server now stamps rules.keeps_running.factory_since when the switch goes on and, after each poll, brings every issue opened since into the matching workstream (its repo source first, else one naming no repo, oldest first). Such a job is never started by labels; the workstream's go-ahead starts it within its amount."
  - "A publish from a computer whose GitHub connection is missing or belongs to someone else was coded no_push_permission and told the person to ask for write access. It is now github_not_connected or github_other_account, with a sentence that says to connect GitHub in Settings › Connectors (naming both accounts), and the workstream task's Ask says Connect GitHub to publish with Retry publishing instead of Go on with #N?."
---

E4 final run, findings F1, F2 and F3.
