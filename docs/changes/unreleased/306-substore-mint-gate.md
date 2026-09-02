---
kind: fixed
title: two mints of one subharness at the same instant can no longer both win
pr: 306
surface: [engine]
invalidates:
  - "The exclusive create on a version record was described as the whole of how a version is claimed, and it is not enough on its own. `substore.Store.Mint` now holds an advisory lock on `<name>/.mint.lock` across the whole read-check-write, and the exclusive create is the floor underneath it."
  - "The package doc said a writer that lost the race for v2 is told the name is taken and mints v3. It is refused, never shifted along: the loser normally reads the winner's version and is told its parent has moved on."
  - "A subharness's directory held version records, version bundles, `memory.md` and `last-run.json`. It now also holds `.mint.lock`, which `Mint` creates when it takes the gate — so a name's directory can exist with no version in it, and a directory with no version is on no list, as it always was."
---

Eight writers minting from v1 at once left two of them believing they had
minted, and the second one recorded v1 as its parent — one version with two
children, which is the forked lineage the refusal in `Store.write` exists to
prevent. `Mint` counted the next version from the record files, where a number is
spent the moment `os.Link` lands, and checked the parent against the head, which
is the highest version whose bundle directory is also on disk. `Store.write`
links the record first and renames the bundle second, so a writer the scheduler
dropped between those two calls read a head that had already moved and claimed a
number nobody was racing it for. The read and the claim are one step now.
