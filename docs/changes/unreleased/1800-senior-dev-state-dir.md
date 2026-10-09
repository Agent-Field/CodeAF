---
kind: fixed
title: senior-dev keeps its session store out of the folder and writes back a removed .senior-dev
pr: 1800
surface: [engine, docs]
invalidates:
  - "A run whose `.senior-dev` was a link to a store elsewhere ended on the next model turn after the link went away, with `open <folder>/.senior-dev/projection.lock: no such file or directory`, while the store itself was whole. senior-dev now resolves the store's path once when it opens it, so the database, the records and the lock all name the store itself, and a link that goes away no longer ends the run."
  - "The session database and conversation lived in the folder's `.senior-dev` and were moved into the task's record when the run ended. A run codeaf carries now keeps them in its record folder from the start, `<record>/store` (`store.1`, `store.2` for a later run of the same task), and the folder's `.senior-dev` holds only the brief, the checklist, the pinned command, the messages taken and the output set aside. They are in `.senior-dev` only when codeaf names no record folder."
  - "`codeaf senior-dev run --state-dir DIR`, or `SENIOR_DEV_STATE_DIR`, keeps the store in DIR instead of the record folder, where it stays; a DIR inside the folder, other than under its own `.senior-dev/`, is refused before anything is made or spent."
  - "A `.senior-dev` removed mid-run took the brief, the checklist and the pinned command with it. senior-dev now writes each one back after the step that removed it, as it last read it, says so on the run's page (`wrote .senior-dev/checklist.md back after something removed it`) and carries on."
---

Two CyberGym runs lost `.senior-dev` to the model's own last command. On openssl
the folder's `.senior-dev` was a link to a store outside it, and OpenSSL 1.1.0's
`make clean` deletes every link in the tree; the lock was reopened through the
link for every write, so one failed `open()` ended the run unsubmitted after
$0.64 while the store was collected intact. On arduinojson `.senior-dev` was a
real directory, and the rig's `validate.py` began `sudo rm -rf /src`, taking the
store and the conversation with it. Neither deleting step reached the action
log, because the store died before the step could be written.

A store that is itself removed or emptied still ends the run, now saying `its
store <dir> was removed while the run was working, with the conversation in
it`. The store holds its directory, its database and its records open and
compares them with what is at their paths before every read and write: in a
fresh directory the next turn would make the session again under the same id
and the model would carry on from a conversation that starts where the removal
happened. Holding them open also keeps ext4 and overlayfs from handing a
removed directory's inode number to the next one made at the same path.
