---
kind: changed
title: A crew manager is told what became of each hand-off, and a paused member holds its work
pr: 2897
surface: [engine, app, chat]
invalidates:
  - "team_send answered every directive with \"A member that is idle starts a turn on it now, and a busy one reads it at its next step\" whatever happened. It now waits up to four seconds on the team's traffic and says for each member one of: \"@web started a turn on it.\", \"@web is in a turn and reads it at the next step.\", \"@web did NOT start: <why>.\", or \"@web has shown no sign of starting yet; team_status says whether it did.\" Where a member did not start, the answer also tells the manager not to tell the person the work is under way."
  - "A member's pause lived only in the desktop app's own file (app/crews.json, members.<crew>/<agent>.paused) and the engine did not know of it: a directive opened a paused member, started its turn and logged \"woke @x\", and the app then stopped that turn, so the directive was consumed and Resume replayed nothing. The pause is now the team's record (teams.Member.Paused, File.SetPaused). A paused member is not opened, roused or woken; the directive stays undelivered and starts the member's turn when the person resumes it. The app carries an earlier build's pause onto the team's record once."
  - "A directive to a paused member wrote \"woke @x\" then \"stopped\" to the traffic. It now writes one line from system, \"held @x: the person paused it; it starts on this when they resume it\", answering the directive, and Resume writes an event from you with the text \"resumed\"."
  - "A member's turn that anybody stopped ended with the event text \"stopped\" (state idle), which neither woke the manager nor was delivered to it. A turn the person stops now ends \"stopped by the person\" (teams.StoppedByPerson); it wakes the manager and is delivered as \"@x's turn was stopped by the person before it finished; it reported nothing, so what you handed it is not done\". A stop by the manager's team_stop still ends \"stopped\" and still wakes nobody. The global manager is not woken by either."
  - "team_status and the manager's digest gave a member's state as running, asking, idle or failed only. A member's line may now read \"paused by the person; a directive waits until they resume it\" or \"on standby; no work handed out yet\", carries the member's role after its handle (\"- @dev, macOS developer\"), and may add \"has not started on #22\" or \"its last turn was stopped by the person\"."
  - "Accepting a crew proposal (crews.create from proposal) started every helper at once on its brief. Each helper now joins on standby (teams.Member.Standby): its start line starts no turn, the traffic says \"opened @x on standby; it starts on its manager's first directive\", and the first directive hands it that line marked \"◆ your standing role from manager (what you are here for, not work to begin; work arrives as a directive)\" in place of \"◆ brief from manager\". team_start and members.start still start a member on its brief at once."
  - "A proposal's role name was kept only as an app-layer role with no instructions, so it reached neither the member nor the manager. It is now also the team's record (teams.Member.Role, File.SetRole): the member's team note says \"Your role in this team: <role>.\", and the manager's roster shows the role in place of the title. members.role writes it too."
  - "The crew proposal prompt asked for each helper's brief as the \"whole assignment\". It now asks for \"what this helper is responsible for\", and says an accepted crew joins on standby."
  - "The manager of a crew could be paused from the app like any member. SetPaused now refuses it: \"the manager of <crew> is not paused; stop its turn instead\"."
---

A manager model reports what its tools tell it. In the measured run a member the
person had paused was handed two directives, the tool said both had started, and
the manager told the person so twice before finding an empty folder. Creating
that crew had also started every helper on its line of the proposal, one of which
began work nobody had asked for.
