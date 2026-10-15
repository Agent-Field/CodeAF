---
kind: fixed
title: codeaf app says crews, live lines and working everywhere, starts a member once, and keeps process ids apart
pr: 0
surface: [engine]
---

Was true: only some `agent.changed` events carried an agent's crews; a crew
member's `line` was always empty; an agent a terminal was working in read
`closed` when the app had closed it; when the app and a terminal both held one
manager, each could open a member for the same `team_start`; a process id was
`<agent>:p<n>`, so after a restart a new `p1` shared the id of the `p1` the
quit stopped; a conversation the app joined from a terminal never streamed a
command's output to the app; and a conversation open in another folder's
terminal could not be joined.

Is true now: every `agent.changed` and every listing carries `crews` and a
live `line`, and says `working` whenever a turn runs; an agent is said again
when it joins or leaves a crew. A start line is claimed in the teams file
(`starts` on a team), so one window carries it out. Process ids are
`<agent>:p<n>.<start>`, and an id naming a process the conversation no longer
runs is answered not found. A joining window's launch is applied as far as one
conversation can carry it for two windows (live command output); the app finds
the host holding a conversation in any folder before refusing to open a second
copy.
