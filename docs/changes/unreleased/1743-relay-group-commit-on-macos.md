---
kind: fixed
title: a relay on a Mac commits a frame's pointers as one group, so a chat's first publish lands in a tenth of a second
pr: 1743
surface: [relay]
invalidates:
  - "A relay running on a Mac made each pointer file durable with its own full disk flush, and a large frame is one pointer per object — the first publish of a chat, which carries the whole workspace tree (a git repository's internals alone are dozens of objects), was the best part of a second behind the call it carried. The durability e2e said so: kill the holder 500ms after a lone call and the other machine could not take the chat at all, because the directory record is created by that first publish. A Mac relay now commits each batch the way syncfs commits one on Linux: every file's data is written with a barrier fsync and one final full sync flushes the disk's write cache, so the batch pays the full flush once instead of per object. The first publish of a new chat now moves the relay head about 100ms after the call completed (was ~700ms), and the 500ms kill mode passes."
  - "flush_other.go no longer builds on darwin; the darwin group commit lives in flush_darwin.go. Other non-Linux platforms are unchanged."
---
