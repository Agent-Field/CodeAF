---
kind: fixed
title: Unfinished saves no longer pile up on disk
pr: 1974
surface: [engine]
invalidates: []
---
The embedded storage engine is furrow 0.3.4. A save that was planned but never finished, because the chat was closed or the engine was replaced mid-save, left a plan file behind for good; on one machine these reached 98 GB. furrow now removes such files once they are ten minutes old.
