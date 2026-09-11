---
kind: fixed
title: plain engine-road chats keep this machine's connection, harness, and profile doors
pr: 906
surface: [chat, engine]
invalidates:
  - "A plain launch through this machine's engine inherited the remote road's missing connections seam, so `/connect` said `connections are unavailable here`. It now opens this machine's account catalog, including connected rows and the model-service group."
  - "A plain launch through this machine's engine had no harness registry and `/harness` said `harnesses are unavailable here`. It now reads the same local registry as an in-process chat."
  - "The consent card's `always` answer on the plain engine road was believed to outlive the session, but no profile write door was present; after the first repair wrote the row, the running gate still did not read it and asked again. The surface now writes the rule and the engine rebuilds that conversation's gate before applying the answer, so the next matching call is allowed without a second question."
  - "A model chosen in `/model` on the plain engine road changed only the running session and was forgotten by the next launch. It is now written through `config.WriteChatModel` to the same profile the engine road resolved."
  - "A model service connected from a plain engine-road chat could not be reached by that running conversation because the daemon retained its boot-time sources. The engine now re-resolves its own profile on every hello and immediately before the existing model-set call is applied, so the next request uses the connected address and key."
  - "The local surface was believed to write the profile its persistent engine used, but the daemon's resolved profile directory never crossed the welcome; a later terminal override could send approvals, model memory, credentials, and services to another directory. The engine's profile directory now travels in the welcome and is the authority for every linked-local read and write."
  - "A model-service key named as `$VAR` was described as immediately live after connection. The persistent engine can read only the environment its own process started with, so the connection receipt now says that explicitly and the manual names the stop-and-relaunch command needed when the variable was exported later. Pasted keys remain live immediately."
  - "Two local processes could update `credentials.json` from the same old snapshot and silently lose one process's connection or disconnection. Every read-modify-write now holds a private cross-process sidecar lock while lock-free readers retain the atomic-replacement view."
---

The local road still borrows the hosted surface builder, then restores only the
doors whose stores and engine are on this machine. `--host` and `--at` keep their
previous absences and no source credential crosses a new wire message.
