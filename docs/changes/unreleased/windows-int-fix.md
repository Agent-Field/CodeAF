---
kind: fixed
title: Windows state replacement works while readers hold the old file open
surface: [engine, chat, resident, remote, factory]
invalidates:
  - "Delete sharing alone did not let Windows replace an open state file. Supported filesystems now replace it while existing readers retain their old generation."
---

Windows 10 version 1709 and newer on NTFS use handle-based POSIX replacement
for files, including spending ledgers held by the usage writer. Unsupported
Windows versions and filesystems fall back to native rename with the existing
bounded retry policy. Directory moves and POSIX behavior retain native rename.

Windows stop tests allow up to two seconds for sockets to finish closing after
confirming process death. Their process checks and product stop behavior are
unchanged.
