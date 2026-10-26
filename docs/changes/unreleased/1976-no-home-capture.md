---
kind: fixed
title: A chat opened in the home folder saves the conversation, not every file in the home folder
pr: 1976
surface: [chat, engine]
invalidates:
  - "A chat whose folder was the home folder, a folder above it or a filesystem root captured that whole folder: the warm capture asked furrow for a plan of every file, a full copy went into the store and each unsealed plan stayed on disk. That folder is now never captured; the chat seals its own folder instead."
  - "Rewind in a chat opened in the home folder restored the folder's files along with the conversation. It now brings back the conversation only, and the chat says so once when it opens."
---

The rule is where the folder is, never what is in it: a folder that is the home
folder, holds it, or is a filesystem root is not a project. Counting files or
bytes to find a folder that is too big would itself be the walk this avoids.
Every seal, rewind and take reads the tree from the same place, so they agree.

On a copy of a real profile used as the home folder, each launch there wrote
about 9 MB of capture plan and added 2.6 to 3.1 GB to the store before this
change, and wrote no plan and under 1 MB after. Time to the first frame did not
move (about 0.45 s), since the capture was already off the paint path.
