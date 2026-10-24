---
kind: fixed
title: quitting while the last upload fails no longer lets another computer open an older save
pr: 1918
surface: [chat, remote]
invalidates:
  - "Quitting released a moved chat's lease even when its final upload failed, so the relay announced a clean quit and another paired computer could continue automatically from the older saved head. Quit now keeps the lease until the last save uploads; the unsent turns stay on this computer."
---
