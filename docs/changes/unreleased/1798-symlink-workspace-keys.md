---
kind: fixed
title: a folder reached through a symlink is one project and one engine, not two
pr: 1798
surface: [engine, chat]
invalidates:
  - "A folder reached through a symlink became two projects, two conversation histories and two engines. `codeaf engine --status --workspace <link>` missed the engine holding `<dir>`, and home's projects panel listed the folder once per spelling. The key-minting doors stopped at filepath.Clean, so each spelling hashed to its own key. Every door that turns a path into a key now resolves symlinks first (filepath.EvalSymlinks, with the original spelling kept when the path does not resolve): engineWorkspace (the engine commands and the host's workspace name, including the default home), enginehost.where (the host directory every lookup, dial, spawn, lock and status is keyed by), v3Workspace (the session bucket and tools root), gitidentity's path fallback (the memory project key), and the typed /project pin. Both spellings of one folder reach one project, one conversation history and one engine."
---

Found in the v0.7.1 happy-path pass on macOS, where `/tmp` is a link to
`/private/tmp`, and reproducible on any system with `mkdir d && ln -s d l`.
`engineWorkspace` — the door every engine command (`--daemon`, `--status`,
`--stop`) and the host's workspace name go through — expanded and cleaned the
path but never resolved its links, so two spellings of one folder keyed two
host directories in `internal/enginehost.where` (sha256 of the cleaned string),
and the person saw one folder while codeaf kept two of everything.

The resolution was first added at `engineWorkspace` alone, and the review of
this pull request caught what that left split: the local surface still hashed
the alias its terminal carried while the daemon listened under the resolved
key, and the default home skipped the door entirely. The identity now resolves
at every layer that mints a key — the host directory inside `where` itself, so
lookup, dial, spawn, lock and status cannot disagree; the default home in
`engineWorkspace`; the session bucket and memory project key in `cmd/codeaf`;
the typed `/project` pin in `internal/tui3`. A regression test drives a symlink
through each layer and asserts both spellings land on one key.

THE UPGRADE KEEPS WHAT THE OLD KEY WROTE. A host directory and a conversation
bucket an older build named by the raw spelling are still this folder's: a
`Dial` that misses the canonical key looks for host directories whose recorded
workspace (`internal/enginehost`'s place file) resolves to the same folder, so
`--status`, `--stop`, `--status-all`, `--stop-all` and the takeover rule reach
an engine the upgrade did not start; and a launch whose canonical bucket holds
no spoken conversation resumes the newest folder in another bucket whose
`meta.workspace` citation resolves to it. No migration, no merge — the old
records are read where they live, and the canonical key takes over from there.

Retiring an alias-keyed host also waits for its original lock, not just the new
canonical lock: the listener can disappear while the old engine is still flushing
its journals. Upgrade regressions exercise status, stop, sweeps and idle/busy
takeover through both spellings.


A spoken alias-keyed conversation also outranks an abandoned empty canonical
folder during hello discovery, matching boot's resume order. Otherwise two plain
hellos could boot two agents onto the same legacy journal. A real-socket regression
asserts both hellos open that journal with one boot.
