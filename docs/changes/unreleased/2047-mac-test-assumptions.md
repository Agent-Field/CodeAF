---
kind: internal
title: The last tests that assumed Linux pass on a Mac
pr: 2047
surface: [build]
invalidates: []
---

The cellstore socket fixtures assumed a short temporary folder that is not
reached through a link; on a Mac it is long and `/tmp` is `/private/tmp`.
The remote stamp answers assumed Linux's device and inode widths. Their
expectations now hold on both systems; no product behaviour changed. The
other Mac assumptions this branch first fixed reached app/v3 through #2099.
