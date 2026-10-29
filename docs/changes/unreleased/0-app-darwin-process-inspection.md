---
kind: fixed
title: macOS reads process groups for left-running services and the job subtree bound
pr: 0
surface: [engine, docs]
invalidates:
  - "On macOS, a server left running by a shell command was never reported, and service records named the shell without its server's argv or listening ports. These records now inspect the surviving processes."
  - "The job subtree CPU and process bound never cut on macOS because subtree usage could not be read. macOS now measures the whole tree and applies the existing bound."
---

On macOS, a registered bash job that sustains more than three fifths of the
machine's logical cores for thirty seconds, or grows past 128 processes for
that long, is now ended like on Linux. The CPU bound applies when no CPU quota
or affinity mask holds codeaf below the machine's cores. Both limits require
15 consecutive readings two seconds apart, and the run is told why it ended.

Process inspection works without cgo, including release and sidecar builds.
Service descriptions read each surviving member's sockets once, select the
first listening member's argv and collect the sorted, unique listening ports.
