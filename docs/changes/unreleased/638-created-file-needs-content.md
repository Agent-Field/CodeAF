---
kind: fixed
title: A file the run created counts as made only while there is something in it
pr: 638
surface: [chat, engine]
invalidates:
  - "A file the session CREATED counted as work it had made as long as the path was still on disk. `Remains.Made` read the created half as `len(reconcile(createdList, tree).kept) > 0`, and `reconcile` settles a created path with an `os.Lstat` and nothing else — so a run that wrote `fix.py`, had one of its own later steps blank it, and then said done read `Made` true over a tree holding an empty file, and with a reader saying nothing was left the door finished there. It fires on any run whose own later step truncates a file it created: a failed rewrite, a generator that produced nothing, a `> file` in a bash step. NOW: `Agent.createdInDeliverable` is the created half of `Made` — under the tree, path resolved so a write through an in-tree link counts as a write to what it points at, a regular file, and above zero bytes. IT IS MEASURED BY SIZE AND NOT BY THE BEFORE-DIGEST #601 ADDED: a created file's `before` is \"\" because the file was absent, while an empty regular file digests to the sha256 of zero bytes, so comparing those two would call the emptied file changed and silently restore the failure."
  - "#601 said that a created file counts exactly as it always did, and that sentence is no longer true — it was the scope of #601 and not a law. What #601 fixed was the MODIFIED half, which settles each path against a digest taken before the write; the created half went on asking only whether the path was there. The two halves now answer the same question in the two ways their ledgers can: a modified file must still differ from what it was, a created file must still have something in it."
  - "`reconcile`, `underTree` and `sweepScratch` are UNCHANGED, and that is deliberate rather than incidental. `reconcile` is also the sweep: a created path that falls out of `kept` while still under the tree goes to `scratch`, and `sweepScratch` deletes scratch — so moving empty created files out of `kept` would have made the harness delete files inside somebody's deliverable. An empty file the run created inside the tree is still kept, still journalled as kept, and is still never touched; what changed is only the reading `Made` takes beside it."
  - "The manual said a file is your work only while its content still differs from what it was before the EDIT, which was an account of the modified half alone: somebody whose run blanked a file it had created got no explanation of why the run carried on. `internal/manual/chat/starting-aforge.md` now says a file the run created counts only while there is something in it — and says in the same breath that it is still your file, because nothing inside the folder aforge is working in is ever deleted, whatever is in it. The section it sat in had grown past the bound a retrieved section is meant to keep, so it is split, and the words somebody would search with (emptied, blank, zero bytes) are in the new heading."
---

Every reading this engine takes of its own work started as a LIST OF PATHS, and
#601 replaced that with a reading of content for the files the session changed.
The files it created kept the old shape, so half the defect stayed: a path is
still not the work, whether the session wrote the file for the first time or the
fortieth.

The measurement is different on this side, and it has to be. A modified file has
something to be compared against — the digest the ledger took before the write.
A created file has nothing before it, so the only honest question is whether
anything is in it now.
