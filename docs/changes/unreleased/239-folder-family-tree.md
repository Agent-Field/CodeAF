---
kind: fixed
title: a task family on a plain folder gets a family tree of its own, and its parts get worktrees off it
pr: 239
surface: [engine]
invalidates:
  - "The parts of a task whose ground is a plain folder worked in the parent's OWN directory, concurrently with each other, with no isolation and no merge — while prompts/task.md and prompts/divide.md both promised each part 'a copy of the repository taken from yours'. Each part now works in a worktree of its own, cut off the mirror, and its branch merges back into the mirror."
  - "The mirror — the private copy of a folder ground made at family start — was a plain directory. It is a git repository now, opened with one baseline commit at the moment it is carved. It exists solely to give parts a worktree source and a merge target; the check is still run against the person's untouched folder with the ledger laid over it, not against this history."
  - "'each part works in a copy of the repository' was the wording everywhere — the two prompts and four manual pages. It is 'a copy of its own' / 'a copy of the parent's working copy' now, because it is true of a folder family as well."
---

A task family is a claim on one body of material, worked in one workspace of
record. That was real for a repository ground and a pile of substitutes for
everything else — and folder grounds are most general agentic work: research
folders, document sweeps, data directories, a report with a section per region.
Those families ran their parts on top of each other.

Opening the mirror as a repository is the whole repair, because every road a
part needs already existed and all of them lead through it: the ground ladder
resolves a part to the tree it is standing in, `prepareTaskTreeOn` cuts the
worktree off that tree's HEAD, and `comeHome` merges the part's branch back into
it exactly as a repository part merges into the person's checkout. No second
merge path was written.

A tree that cannot be opened — no git, a read-only disk — still runs the work,
and says so in the job log and in the parent's own brief rather than letting the
parts find out by writing over each other. `in place` and `folder` families stay
out of scope by ruling: the person said "here", so the family tree is their own
directory and nothing initialises a repository in it.
