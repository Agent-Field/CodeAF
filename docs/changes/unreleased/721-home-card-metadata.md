---
kind: fixed
title: Home-card terminal checks follow the current layout and asynchronous Git reading
pr: 721
surface: [chat, docs]
invalidates:
  - "The live home-card test treated an omitted Git clause as missing metadata even when its temporary folder path exhausted the card. It now uses a short repository path, waits for the reading, compares it with Git, and checks narrow and wide layouts."
  - "The terminal test read the card from column 134 at 180 columns. The current card starts at column 124; complete facts and action labels are checked there."
---

The actual DeepSeek Flash conversation remains part of acceptance. Its answer is
requested as an English word so terminal padding beside the sidebar does not
hide a correct numeric reply from the test. This scenario uses the local
conversation door with one model; engine-host startup is covered separately.
The manual now states why a long path can suppress repository details even in
a wide terminal and uses the card's comma-separated repository clause.
No production layout behavior changed.
