# Workspace limits on Linux

## Are chats sandboxed — network access, installs and writes outside the workspace

New interactive chats are not sandboxed. Their commands run on this computer with the network and the same access as any program you run, including writes outside the workspace and the computer's ordinary temporary folder. Moving chats keeps that access.

Runs with nobody at the keyboard are limited: factory steps, `codeaf chat --once` and `codeaf exec`. Their commands keep the network but may write only their own checkout and their own temp and cache folders. They cannot read keys and sign-ins or use docker. On Linux this uses Landlock, and user namespaces where the system allows them. "What an unattended run may touch" says what is blocked, how to allow more with `"jail.allow"`, and what a computer without them says. `/setup` is for preparing tools on this machine after a move; ordinary commands do not need it to reach the network. Command approvals still apply.

## Why commands fail on Ubuntu 24.04 — user namespaces, AppArmor and reduced workspace limits

New chats declare `host-bound` and do not enter the Linux jail, so user namespaces and AppArmor's namespace restrictions do not limit their commands. An existing cell made by an earlier build keeps its declared class when reopened or moved here. A cell that already declares `sandboxed` still uses its earlier workspace and network limits; codeaf does not rewrite its metadata.

For those existing sandboxed cells, codeaf probes the exact workspace helper once per process. If namespace or helper capabilities are unavailable, a call whose policy permits network access runs unconfined and says once:

`This system cannot apply all workspace limits; commands with network access run without full confinement.`

A sandboxed call that requires the network to be blocked instead refuses:

`this command was not run: the workspace is sandboxed (no network) but this system cannot block the network: user namespaces are disabled; enable user namespaces or declare the workspace host-bound`

When namespaces work but Landlock is missing, network limits still apply and filesystem limits are reduced. These are rules for cells already declared sandboxed, not the limits of a new chat.
