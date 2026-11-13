---
kind: changed
title: The embedded storage engine is furrow 0.3.3, which opens a large store without scanning it
pr: 1962
surface: [engine]
invalidates: []
---
furrow 0.3.3 validates a store's pack checkpoint with one index seek instead of counting every object, opens a settled store without taking the pack lock, and answers `status` without writing. Saving and attaching no longer slow down as the store grows. A compat test seals with furrow 0.2.0, then the embedded engine, then 0.2.0 again on the same store, so existing stores keep working in both directions.
