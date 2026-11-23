---
kind: changed
title: Factory steps, chat --once and codeaf exec keep their commands inside the job
pr: 2495
surface: [factory, engine, chat, docs]
invalidates:
  - "A factory step's commands ran with the person's full permissions, and CODEAF_FACTORY_POSTURE unset meant exactly what --yolo gives. The posture is unchanged, but every command a step runs is now fenced (internal/executor's fence.go). It may write only the worktree, the repository's git folder (hooks and config read-only) and its own temp and cache folders under ~/.codeaf/jail/<id>/. It cannot read ~/.ssh, ~/.aws, ~/.config/gh, the keychain or codeaf's own secret files, and it cannot reach the docker socket. The network stays on."
  - "codeaf chat --once (including --once through a host's headless conversation) and codeaf exec ran commands on the host seat, unconfined. They now run on executor.FencedSeat. Interactive chats are unchanged and still run on the host seat."
  - "internal/executor's jail was used only by sandboxed cells and denied the network. It now also has a Fenced class: network open, writes fenced, secrets hidden. A door selects it by setting session.Config.Fence, and nothing selects it by workspace declaration."
  - "On Linux the jail needed user namespaces, and stock Ubuntu (apparmor_restrict_unprivileged_userns=1, true on this org's Linux boxes and spark) refuses them, so the jail did nothing there. The fence falls back to Landlock alone: writes are limited, and secrets are kept unreadable by granting everything around them. Docker is only steered with DOCKER_HOST, and the checkout's own .git hooks stay writable. It says FencePartialLine once."
  - "The session's read, write and edit tools ran in-process with no limit. In a fenced run they now refuse paths the fence refuses, with the same sentence."
  - "There was no way to widen what an unattended run may touch, because nothing narrowed it. Now \"jail.allow\" widens it: docker, an absolute or ~/ path, or off. It is read from the repository's .codeaf/config.json (a worktree's source checkout), from the profile's config.json, or from CODEAF_JAIL_ALLOW, and the words from all three are added together."
---

On the factory machine called dumb, a step ran `brew install zig` (1.6 GB) and pulled docker
images that nobody asked for. Before this change nothing stopped a step from reading `~/.ssh` or
writing outside its repository. A command that is refused now reads one sentence:
`codeaf blocked this: …`. A system that cannot confine commands says so once and runs as
before, and no step fails because the fence could not be applied.
