---
kind: changed
title: Check catches shallow apps and record pages that point nowhere
pr: 2994
surface: [app, engine]
invalidates: []
---

An app could pass Check with a record page bound to an ID no record has, or with examples and automations that never touched the records they described. Check now names the record page whose target does not exist, reports how deep the app's sample data and screens go beside its other findings, and checks that each example and run belongs to a real record.
