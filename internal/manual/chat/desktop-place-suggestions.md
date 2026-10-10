# Desktop place suggestions

## Why suggest merging or archiving an old place, and how long does Not now hide it?

A place untouched for 60 days can be offered for merging or archiving. Visits and chat activity in the place or its children keep it current. Pinned, archived, running and waiting places are left alone. An unknown age is never guessed.

This date rule makes no model call. The suggestion does not merge, archive or delete anything by itself. Not now hides that place's cleanup suggestion for 30 days across desktop windows. It can return exactly 30 days later if the place stays idle. Snoozing does not change the place graph or invalidate Undo.

GET /places/suggestions returns a suggestions array with kind "mergeOrArchive" and placeId. POST /places/suggestions/snooze accepts {"placeId":"…"} and returns the snooze's until time. Both routes require the desktop engine token and shared snooze storage. They do not generate filing or cluster suggestions.
