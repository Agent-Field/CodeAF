---
kind: fixed
title: Preserve native text and paths and skip headless login profiles
pr: 0
surface: [chat, engine, resident]
invalidates:
  - "Headless foreground commands previously loaded a login profile each time. They now use non-login bash and inherit the launching environment, like chat."
  - "BOM-marked UTF-16 files previously reached file tools as raw bytes. Reads now decode them, and edits, rewrites and appends preserve their encoding and BOM."
  - "Headless edits and appends could mix LF into CRLF files. File tools now share the chat edit's line-ending preservation."
  - "Folder browsing and report discovery previously assumed slash-prefixed paths. They now accept native Windows drive and UNC paths."
---

Detached background jobs, services and command health checks retain login
shells so their login profiles can supply PATH. Health checks use the shared
native bash resolver and pipefail, selecting Git Bash on Windows.

Model-visible command output normalizes CRLF to LF while retaining lone
carriage returns. Patch detection accepts both historical `bash -lc` and
non-login `bash -c` invocations. These text rules apply on every host OS.
