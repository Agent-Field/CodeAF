---
kind: changed
title: the Slack thread limit is written down once, and the description a model reads interpolates it
pr: 1371
surface: [engine, remote]
invalidates: []
---
The number 15 was written three times for one limit: as the word "fifteen" in
SlackReadThread's doc, as "15" in the query parameter that actually applies it,
and as "15" in the tool description a model reads, in a different package. The
model-facing copy is the one that goes stale in silence, because nothing fails
when it is wrong: the call still returns fifteen messages and the model still
believes whatever the sentence said. Now connect.SlackThreadLimit is the one
place, the parameter uses it, and the description concatenates it.

The description is still a compile-time const, not a string built at init. Both
operands are string constants, so "up to " + connect.SlackThreadLimit +
" messages" folds at compile time and the declaration stays usable everywhere a
const is. Nothing moved into a function, and the rendered text is byte for byte
what it was.

Setting.read now carries the same rule for settings rows, which is where the
argument has teeth: a row's read returns what the product will actually use and
not what is stored, so a person who types a value the resolver will not honour
watches it change in front of them. That is what makes a lossy store such as
persistedCount safe, and it is the only thing that does, because nothing
downstream of the row ever sees what was typed.
