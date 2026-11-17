---
kind: fixed
title: Fold keeps the destination launch plan and Undo restores the source plan
pr: 2786
surface: [factory, desk]
invalidates:
  - "Fold tried to move both workstreams' singleton plan rows under one key, so two saved plans caused a storage conflict. The destination now keeps its launch plan; the source plan stays under its original key in the Undo receipt and is restored with the source. Other workstream parts still move."
---
