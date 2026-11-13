---
kind: fixed
title: An idle terminal no longer reopens the key vault once per shared key
pr: 1980
surface: [chat]
invalidates:
  - "A home-state restore asked the vault about each shared key in turn and opened the whole sealed vault for every answer. It now reads the scope's removals once per restore."
  - "The background copier tried an older chat whose copy failed again on every pass. A failed copy now waits until the chat changes, the next launch, or the person opening it."
---
With 963 shared memories and a 1.5 MB vault, a restore decoded about 1.4 GB of
JSON. A terminal that pulled every round sat above one core while nobody typed.
Measured on Spark against a copy of that profile, with the vault moving every
round, the client went from 111 to 115% CPU down to 15%. Without that, it went
from 15% to 11%.
