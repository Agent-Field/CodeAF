# swe-pro

## What /swe-pro does — hand one large change to swe-pro, an autonomous coding agent

`/swe-pro <brief>` hands the whole brief to **swe-pro**, an autonomous coding agent that
runs on this machine as its own program. codeaf gives it a copy of your folder, this
conversation's dollar and time limits, and your OpenRouter key; swe-pro maps the
repository, pins a test command, edits, runs the tests, and freezes a candidate it has
verified. When it ends, its work is squashed into one commit on your branch.

Use it for one change that is big enough to want an agent of its own for an hour and is
specified well enough that nobody will be asked anything: a rewrite across a package, a
migration, a feature with tests. A change you would do in a few steps is not worth it.

## What swe-pro cannot do — it cannot ask, it has no step cap, it reports its own checking

swe-pro runs unattended. A question its model tries to ask is turned down inside the
program, so put everything it would stop and ask into the brief: the files, the
constraints, the wrong answer to avoid, how to check the result.

It has no step cap. It is held to the dollar and hour ceilings codeaf hands it on its
command line, and codeaf stops it from outside at the same limits. On its task page the
step count is what it reported.

Its result keeps two things apart: what its model claimed it did, and what swe-pro itself
observed when it ran the project's tests on the frozen tree. Read the second for "did it
work".

## Where its work goes and what it costs

swe-pro commits after every edit inside its copy. At landing those commits are squashed
into one commit whose subject is the task's title and whose body is swe-pro's account of
the ending, and that commit is merged into your folder. Its spending is folded into this
conversation's total as it reports it, under the name `swe-pro` on the spending page.

## Installing it — why there is no /swe-pro here

The row exists only where the `swe-pro` binary is on PATH. Put this file and
`swe-pro.json` in `~/.codeaf/delegates/` and start codeaf again. `/delegate` says what it
found.
