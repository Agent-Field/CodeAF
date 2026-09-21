---
kind: fixed
title: a watch stopped for permission says the line, and no longer says a person asked
pr: 1314
surface: [chat]
invalidates:
  - "A standing row whose watch stopped for want of permission read `asks: stopped: it needed your ok to run ...`, which said somebody had asked a question. Nobody had. The row now says the line bare."
---
standing.Item.NeedsPerson carries two different things and its own doc says so:
a question a firing put to a person in its own words, or the line written when a
call was refused for want of somebody to allow it. The switcher's note put the
ask word over both. It now asks standing.IsPermissionLine, the one predicate
that knows the difference, and says a refusal bare, because the refusal is
already a whole sentence about the item.
