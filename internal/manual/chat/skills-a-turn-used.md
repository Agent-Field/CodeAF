# Skills a turn used

## Which skills did it use?

When a turn carries skills, a dim line in that turn names them:

```
skills · linter, release-check
```

It sits directly under your message and stays there after the answer lands:
the turn's steps fold into the `▸ worked` line below it, and this line is not
folded with them. `codeaf chat --once` prints the same record as
`skills carried: linter, release-check`.

Those names come from the turn's skill list, not by taking apart the words in the
line. The row is a record of what that turn carried with it. It is not a warning,
a question or work waiting for you, so it has no attention mark, count or action.

## Did it use my skill?

If your skill's name is in that line, the turn carried it. The line belongs to that
one turn; it is not a list of every skill on the shelf and does not say what a later
turn will carry.

No line does **not** mean "no skills used." Older peers do not send a skill list,
and an absent list and an empty list arrive with the same uncertainty. codeaf can
therefore draw a non-empty list, but it cannot honestly turn silence into a claim
that the turn used none.

A message that carries a picture draws no line because it carries no skills, the
ones you attached with `/skill` included. The attachment stays on, and your next
message without a picture carries it again.

## Why is that line under my message?

The skills belong to the turn your message opened, so their row sits with that
message rather than with the answer or with provider status, and the fold that
hides the turn's steps starts below it. It is dim on purpose: it tells you
what the turn carried after the fact, and there is nothing to approve, answer or
fix.

A skill the model opened by itself with `use_skill` shows as that tool call among
the turn's steps, not in this line: the line names only what the turn carried
from the start.
