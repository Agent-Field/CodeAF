---
kind: fixed
title: Landing a task branch permits unused Git filter drivers
pr: 1995
surface: [engine]
invalidates:
  - "7ad5103348 refused a land for any configured filter program, including an unused global or system git-lfs driver. A land now refuses a filter only when an attribute selects its configured program for a path the land writes or compares."
---

Filter refusals retain reason `filter` and name the affected paths (at most
50). Git resolves attributes from the checkout, target and task trees, merge
base, nested `.gitattributes`, `info/attributes`, and global and system attribute
files. Tracked paths compared by the clean-checkout check are included, as are
paths in any checked-out copy of the task branch, using that copy's own Git
configuration. The check happens before status or merge preflight can run a
filter.

The plan binds attribute answers and readable attribute files outside the
trees, alongside the existing whole-configuration digest. Land revalidates
them under the existing lock and refuses `changed` if they move. An unused
driver does not get excluded from that configuration binding.

Unknown attributes still refuse: unsupported Git flags, failed reads, malformed
or incomplete answers, and warnings accompanying a successful exit are not
permission to land. The existing merge preflight needs Git 2.38; reading tree
attributes needs 2.40, and resolving global and system attribute-file locations
needs 2.42. These questions are asked only when some filter driver has a
program configured; with none, a land asks Git nothing about attributes and an
older Git lands as before. With a program configured, an older Git that cannot
answer them refuses.
Path lists and each attribute answer or diagnostic stream are bounded at
8 MiB; readable attribute files outside the trees share an 8 MiB budget per
checkout. Oversized reads refuse `too_large`. Merge drivers, sparse checkouts
and target-branch merge options retain their existing refusals.
