---
kind: changed
title: persistedCount says in its own doc that it destroys the difference between unset and zero
pr: 1369
surface: [engine]
invalidates: []
---
A key nobody wrote and a key written as 0 both come back 0 from persistedCount,
so no caller downstream can tell which it was. Every caller today feeds
ctxbudget.Limits, where 0 means use the default and the settings row reads back
the resolver, so a person who types 0 watches the default appear in the row and
the loss is visible to them. That is what makes it safe where it is used now,
and nothing said so. The doc comment now states the law for the next caller,
which is that anyone needing to tell unset from zero uses persistedInt and
decides at the call site.
