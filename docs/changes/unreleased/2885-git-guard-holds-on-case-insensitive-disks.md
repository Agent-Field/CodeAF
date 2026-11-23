---
kind: fixed
title: the guard on a folder's git history now holds on disks that ignore case
pr: 2885
surface: [engine, docs]
invalidates:
  - "Was true: the guard that stops a task or `codeaf do` worker from deleting, moving or rewriting the `.git` of the folder it was given compared names exactly, so on a disk that ignores case (the Mac default) `rm -rf .GIT`, `mv .Git x`, `rm -rf .git/OBJECTS`, `echo x > .git/head` and `sed -i '' ... .GIT/config` were allowed, history was destroyed or rewritten, and the run reported success. Is true now: those spellings are the same entry as `.git` and are refused with the same `is outside your copy` sentence, including when the folder itself is spelled in another case. On a disk that tells upper and lower case apart nothing changes, and a separate folder named `.GIT` is not treated as the repository's history."
---

Whether a disk ignores case is read from the disk itself, not from the operating
system. An ordinary write inside the folder through an absolute path spelled in
another case is still answered with the older `is outside your copy` refusal;
that is a separate, older gap and is unchanged here.
