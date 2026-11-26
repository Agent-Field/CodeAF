---
kind: added
title: Pack, install and update apps with a change card
pr: 2970
surface: [app]
invalidates:
  - "Kept apps previously stayed in their own space. A package now lets another space install the definition against its common types."
---

The install card shows requested access and reuses existing records by common
type. Updates merge the publisher's new release with local edits and put any
competing changes on the card. Apply publishes the checked result. Forks keep
their own definition history and record where they came from.
