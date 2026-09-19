---
kind: fixed
title: a place sweep note can no longer write after the process it belongs to closes
pr: 1227
surface: [chat]
---
The chat process starts a place sweep in the background; on an error the sweep
resolves the current home and appends to sweep.log. The sweep was started and
forgotten, so a process or test that closed quickly could let that write land
after close, under a directory already being torn down, which is the cleanup race
that leaves a directory not empty. Close now seals the note under the sweep lock
before it closes shared state: a note already in flight finishes, no note writes
after the seal, and close does not wait for the walk itself, so quit does not grow
with the number of conversation folders the machine has held. The property test
blocks the sweep as an unbounded walk and asserts close returns within a small
fixed bound while a note attempted after the seal writes nothing.
