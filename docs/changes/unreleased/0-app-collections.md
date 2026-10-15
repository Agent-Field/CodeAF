---
kind: added
title: codeaf app serves collections, and the collections store moves to schema 2
pr: 0
surface: [engine]
invalidates:
  - "Collections were reachable only through `codeaf collections`. codeaf app now serves collections.list/create/rename/duplicate/delete/add/remove/open/expand and the collection.changed event, carries the hello feature \"collections\", and answers what each member is now (live, deleted, missing or unknown) without storing it."
  - "The collections database was schema 1 and a collection could not be deleted. Schema 2 records when a collection was made and when each membership was added, adds the crew and process kinds, and cascades deletes; a schema-1 file is read as it is and migrated in place by the first write, after which a schema-1 build refuses it as an unsupported collections database."
  - "Two collections could share a name. Names are now unique without regard to case (Unicode case folding); a taken name answers conflict on the wire and `another collection already has that name` at the command line."
  - "`codeaf collections` had no delete or duplicate verb. It now has both."
  - "collections.list stopped at 1000 collections and collections.open at 500 members with nothing saying more remained past the list. Both now page: an opaque `next` cursor passed back as `after`, and collections.list answers `total`; a walk never repeats or skips a row that existed when it began."
  - "collections.expand skipped a member whose owner could not be asked without counting it anywhere. It is now counted in `unavailable` (never in `missing`), the first 50 are listed in `unavailable_refs` with a `target.reason`, nested collections past `depth` are counted in `unread`, and a target whose state is unknown carries a `reason` everywhere."
  - "A member whose thing moved could only be removed and added again, losing its place. collections.relink puts the new reference in the old one's place and moment in one write, after checking the new one is there now."
---

Deleting a collection takes its memberships and every membership naming it,
never the records they referenced. collections.open resolves members with at
most one listing per owner per call, and a file inside a git work tree carries
its repository, branch and path, read from .git without running git.
