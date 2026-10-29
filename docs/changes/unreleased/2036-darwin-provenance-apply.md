---
kind: fixed
title: Private task review and apply work on macOS despite the system provenance attribute
pr: 2036
surface: [engine, chat]
invalidates:
  - "Portable task review and apply refused any path carrying an extended attribute on every platform. On macOS they now accept the system's own com.apple.provenance attribute (11 bytes), which macOS adds to everything a tracked process writes; every other attribute, an ACL or a native file flag still refuses."
---

macOS 13 and later attaches `com.apple.provenance` to every file, directory and
symlink a provenance-tracked process writes, which includes every locally built
binary and the tools it runs. So `/task review` and `/task apply` refused every
file on a Mac with "portable apply cannot preserve extended attributes or
access-control metadata".

On macOS the check now allows exactly that one attribute with an 11-byte value,
read without following links and listed again after the read. It is not carried
over: an applied file gets the provenance macOS gives it. Quarantine, `macl`,
FinderInfo, resource forks, `metadata:*` names and every other attribute still
refuse. The macOS ACL check also refuses ACL entry lines explicitly, because
`ls -l` shows `@` rather than `+` when a path has both. Linux is unchanged.
