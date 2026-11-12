---
kind: added
title: codeaf app can say who holds a port and which port is free
pr: 0
surface: [engine]
invalidates:
  - "The app could only learn a port was in conflict from a service's health check, with no holder and no free port to offer. ports.inspect {port} and ports.suggest {after} now observe the machine: state free, in_use or unknown, the holder's pid and name when proved, and a port that was free when bound. Neither reserves anything."
---

New methods `ports.inspect {port}` and `ports.suggest {after}` over the new
generic `internal/ports` package. A check binds and releases the IPv4 and IPv6
wildcards and loopbacks; only a refused bind that says the address is in use is
`in_use`, and a refusal or deadline is `unknown`, never free. The holder comes
from the system `lsof` with fixed arguments, 500 ms and 64 KiB at most, and is
omitted unless exactly one process is named. Suggestions start at 1024 or
`after+1`, look at no more than 128 ports in about a second and never wrap.
