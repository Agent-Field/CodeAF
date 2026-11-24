---
kind: changed
title: One access core decides who may view, comment on, edit or fully govern every organization item
pr: 2632
surface: [factory]
invalidates:
  - "An organization workstream's share is always a writer, and only teams can be shared with."
  - "The operator sees every person-owned workstream and hears every workstream event."
---

`internal/factory/serve/item_access.go` is the one place an organization item's access is decided. An item has one owner (a person, or a team whose members all hold it), shares to people or teams each with a role `view`, `comment`, `edit` or `full`, and may be private. The owner holds `full`; several shares reaching one person give the highest; a private item is its owner's alone; the operator views items that are not private and sees only the existence of private ones, and is never an editor by being the operator.

Workstreams now read their access through it. A workstream kept with the old teams-only `shared_with` reads as those teams shared for `edit`, so nothing anyone could do before is lost. Workstream events go only to who may view the workstream before or after the change, which for a private one never includes the operator.
