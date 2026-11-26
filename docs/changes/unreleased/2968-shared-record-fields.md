---
kind: fixed
title: Show and edit shared records in Desk app lists
pr: 2968
surface: [app]
invalidates:
  - "A Desk list could receive shared records but draw no fields because the local app declaration was empty. The engine now supplies visible field descriptors under the app's local alias, and Desk uses them to render the list and open the record editor."
---

The kept app and its isolated sample preview resolve the schema in the same
scope as their rows. A person who can write the shared type can edit an email
from Support and see it change in an already open CRM list. The writer
keeps the app, person and cause in the record's history.
