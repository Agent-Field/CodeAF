---
kind: internal
title: The last tests that assumed Linux pass on a Mac
pr: 2047
surface: [build]
invalidates: []
---

Twelve tests assumed a Linux userland: BSD `mktemp` ignoring `TMPDIR`
without a template, `/tmp` linking to `/private/tmp`, a long per-user
temporary folder, bash 3.2 calling an empty array unset, a Mac's longer
device and inode numbers, and the `opt+` chord spelling. Their probes and
expectations now hold on both systems; no product behaviour changed.
