# Workspace limits on Linux

## Why commands fail on Ubuntu 24.04 — user namespaces, AppArmor and reduced workspace limits

Some Linux hosts allow a user namespace but refuse to start codeaf's workspace helper with the capabilities it needs. Ubuntu's AppArmor restrictions can do this. codeaf tests that exact helper launch once per process, before using it; drawing the screen never runs the probe.

When those limits are unavailable, a command whose workspace policy allows network access (including a setup call) runs unconfined. The conversation says once, as a dim line:

`This system cannot apply all workspace limits; commands with network access run without full confinement.`

A sandboxed command that requires the network to be blocked does not run. It says:

`this command was not run: the workspace is sandboxed (no network) but this system cannot block the network: user namespaces are disabled; enable user namespaces or declare the workspace host-bound`

This also applies when the helper cannot use namespaces because of the host's security policy. Enable the required user namespaces and helper capabilities through the host's security settings, or declare trusted work host-bound. A host-bound workspace runs with the host's access. When namespaces work but Landlock is missing, network limits still apply; filesystem limits are reduced and the same dim line names that limit.
