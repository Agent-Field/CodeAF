---
kind: fixed
title: one wake per member reply, team_stop reaches background members, a demoted manager loses its verbs
pr: 1514
surface: [chat, engine, remote]
invalidates:
  - "A member's reply to its manager woke the manager, and the finished event of the same member turn woke it again seconds later, so the ten-wake loop breaker tripped after about five real rounds. One reply is now one wake, and ten wakes are ten replies."
  - "team_stop did not stop a member codeaf opened in the background with no window on it; the tool and the manual said so. The member's own session now performs its manager's stop, and the tool and the manual say it stops."
  - "A conversation removed as a team's manager kept every manager verb on its belt, each refusing when called (team.go said this was deliberate). The verbs now leave at its next step, and a remembered call is told the role went."
  - "Closing a sub-team whose manager alone was working closed it at once while the manager kept running. The card now opens with Wrap up first leading, and Close now stops that manager's turn but keeps its tab."
  - "A team's daily spend that could not be read counted as $0, so the cap stopped holding. A capped team now starts nothing new until the spend can be read, and says why."
  - "A team's packet file rotation kept only waiting packets, so a decided answer lived one more rotation and was gone. Answers not yet handed to their asker, and today's cap decisions, are now carried across rotations."
  - "Teams.Name and Teams.Propose with a zero or negative budget gave the engine's model call no deadline. Zero now means the engine's 30-second ceiling, and a negative budget makes no call."
---
