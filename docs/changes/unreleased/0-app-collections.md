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
---

Deleting a collection takes its memberships and every membership naming it,
never the records they referenced. collections.open resolves members with at
most one listing per owner per call, and a file inside a git work tree carries
its repository, branch and path, read from .git without running git.
