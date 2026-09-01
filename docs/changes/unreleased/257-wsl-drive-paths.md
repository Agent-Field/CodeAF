---
kind: fixed
title: a file dragged from Windows into a WSL session becomes a picture, not a path in the message
pr: 257
surface: [chat]
invalidates:
  - "A drop from Windows Explorer into a WSL terminal arrived as `'c:/Users/you/Pictures/Screenshots/Screenshot (1).png'` and was sent to the model as text. Inside WSL, `X:/…`, `X:\\…`, `file:///X:/…`, `\\\\wsl.localhost\\<distro>\\…` and `\\\\wsl$\\<distro>\\…` are read at `/mnt/x/…` (or the root `/etc/wsl.conf` names) and land on the tray like any other drop."
  - "The manual never said what happens to a picture in the clipboard. It says now: aforge does not read pixels from the clipboard; drag the file in or paste its path."
---
