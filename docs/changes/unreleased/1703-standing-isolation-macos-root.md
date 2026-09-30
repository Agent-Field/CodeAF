---
kind: fixed
title: the standing isolation test compares the canonical repository root, so it passes on macOS
pr: 1703
surface: [engine]
invalidates: []
---

The recorded root was right all along; the test compared it with the raw temp
directory, which macOS spells through the /var to /private/var symlink.
