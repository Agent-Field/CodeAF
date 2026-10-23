---
kind: added
title: the teams page draws a team's total live cost of its own
pr: 1878
surface: [chat, remote]
invalidates:
  - "The teams page used to draw one money figure per team: the cap reading `$1.20 of $5 today`, dim, in the header strip. It now also draws the team's own total live cost — all of it, all time, every team under it, whole conversation chains, money following the conversation across membership changes — on each rail row, in the pane for the selected team, and in the team's Settings card beside each member's share; the cap reading stays where it is."
  - "A member whose conversation left a team used to vanish from that team's money as of the next day. A removal now stamps the end of the membership, the team keeps the member's spend while they were in it, and nothing after the removal counts."
---

A team's total is a read over the same ledger and teams file the day's spend
comes from, so it answers the same stamp and costs no extra disk read: `Teams.Total`
over --host behind the new `Welcome.TeamsTotal` flag, with no totals drawn when
an engine does not answer it.