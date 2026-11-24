---
kind: added
title: A person who leaves hands their open tasks on; a changed file stops the tasks that read it
pr: 2673
surface: [factory, app]
invalidates:
  - "Removing a person from an organization workstream left their open tasks owned by someone who could no longer see them; they now go to the workstream's fallback, else its steward, with \"Handed to <who>\" in each thread."
  - "Changing a file's contents only named its used_by tasks in the event; each running one now stops on a question to its owner, \"<file> changed while this ran. Carry on?\"."
---

A Factory task cannot be stopped by a file change, because its state is its
job's; its thread gains the line instead. A rename alone flags nothing.
