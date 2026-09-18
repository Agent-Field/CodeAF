---
kind: fixed
title: a bare auto row under picked from = catalog reads the catalog's figures alone
surface: [chat, engine]
invalidates:
  - "Under `picked from = catalog` a tier row that says `auto` answered with the Model Pool's measurements blended in while its rung said `computed from the catalog`; it now answers the catalog's published figures alone, the same computation the pick word's own seat runs."
  - "The prior decision lived in two places — `AutoPick`, which always carried the measurements, and `pickedModel`, which carried them only under `learn`. Both seams now read one helper, `priorFor`, so a bare `auto` row and the pick word's own seat cannot disagree about what `catalog` means again."
---
