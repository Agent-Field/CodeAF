---
kind: fixed
title: First standing approval preserves the shared timer's owner
pr: 1622
surface: [engine]
invalidates:
  - "Approving the first standing item in a new profile could take another profile's OS timer despite launch-time ownership protection. Implicit setup and repair now check ownership under the same interprocess lock as explicit settings changes. Another owner is retained and a blocked setup is reported honestly."
---
