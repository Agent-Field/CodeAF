---
kind: fixed
title: codeaf exec and saved harnesses refuse to read the profile key, wherever the profile lives
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: the credential refusal stood on the chat's and the resident's doors, but `codeaf exec` (and every unattended run and benchmark built on its leaf) and a saved harness's tool calls ran without it, so an agent run through `codeaf exec` with `CODEAF_HOME` pointed at a temporary folder could `cat` that folder's `config.json` and send its `api_key` to the model. Is true now: both doors ask the same floor before any tool runs and answer `Blocked: this call reaches for a credential …`."
  - "Was true: only the homes the running process could name (the default, `CODEAF_HOME`, `CODEAF_PROFILE_DIR`) had their key files protected. Is true now: a home a command line names for itself (`CODEAF_HOME=/tmp/h cat /tmp/h/config.json`) is protected too, and any `config.json` whose top level holds a non-empty `api_key` is refused in any folder. A project's own `config.json` with no key still reads."
---
The floor is `internal/approval/credentials.go`; the two doors are
`internal/exec/tools.go`'s `Toolbox.Execute` and `cmd/codeaf/chatv3_harness.go`'s
bridge. The page is `permissions.md`, under "Is a profile in a temp folder protected too".
