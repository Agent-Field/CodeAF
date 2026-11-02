---
kind: internal
title: the stage turn test covers an intermediate report and a final report that ends the turn
pr: 2177
surface: [engine]
---

`TestATurnThatMustReportRequiresTheVerbOnItsFirstRequestOnly` was red from
the commit that added it: its report was final, and a final `stage_result`
ends the turn, so the second request it expected never came. It now reports
with `final` false. A new sibling test holds the final-report road. Production
code is unchanged.
