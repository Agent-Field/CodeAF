---
kind: fixed
title: Quitting waits briefly for lane notes to finish saving
pr: 2457
surface: [engine]
invalidates: []
---

Quitting used to cancel the lane-note writer without waiting for it. A save
already in progress could keep writing after the exit path returned. The exit
now waits for that writer and its final flush, for about 50 milliseconds at
most. If saving takes longer, one plain line reports the timeout and quitting
continues. A slow or stuck lane-note save cannot hold the program open.
