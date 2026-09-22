---
kind: internal
title: the stopped frame's helper says it exists in order to be shared
pr: 1374
surface: [chat]
invalidates: []
---
The guard on the stopped frame's comparison works only because it calls the same
function the real test calls. The comment said the function must stay the whole
frame and that narrowing it turns the guard red. It did not say why the function
exists at all, so inlining it back into its callers reads like removing a
pointless indirection, leaves every test green, and detaches the guard from the
comparison it guards in the same stroke.

The comment now says it. A guard nobody can see the shape of is a guard somebody
tidies away.
