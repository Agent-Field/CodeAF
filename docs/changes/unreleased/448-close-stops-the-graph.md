---
kind: fixed
title: quitting stops the task graph, so a node admitted a moment before never runs on unreachable
pr: 448
surface: [engine]
invalidates:
  - "A node's context and cancel used to be made inside its own goroutine, by Agent.runTaskNode. They are made by TaskGraph.runFrontier now, in the same hold of the graph's lock that marks the node running, so a node is reachable from the moment it is admitted."
  - "TaskGraph.stop used to learn whether anybody was running a node by asking whether its cancel was nil. That no longer means anything — every running node has a handle now — and the fact is written down as TaskNode.claimed instead, taken by TaskNode.claimRun under the same lock the stop reads it under."
  - "Agent.Close used to reach running nodes only through the jobs round, which walks the job registry. It calls TaskGraph.stopAll(jobShutdownGrace) beside that round now, and a closed session has no running nodes."
  - "jobRegistry.newJob used to accept a job at any time. After jobRegistry.shutdown it refuses with 'this session has closed; nothing new starts in it', so nothing registers into a session that has left."
  - "jobRegistry.add used to return nothing and always append. It returns an error now, and the check for a closed registry happens under the SAME hold of the lock as the append (jobRegistry.join) — newJob's check cannot be the one that makes the law true, because it releases the lock to create the log. A job refused at that door removes the log it had claimed, and every caller undoes what it had already done: the forked process is killed, a hand's out-count is lowered, a watch's slot is released."
  - "Agent.Close used to return nil immediately for any caller that found the session already closed. It sets that flag before it cuts anything, so a second concurrent caller was told the session had closed while its turn, its nodes and its jobs were still running. Close keeps a closeDone channel now, closed as its last act, and a caller that arrives during a close waits for it to finish."
  - "TaskGraph.stop used to hand no slot back on any road, and its own comment said so. That is still true of a queued node, which never took one; a RUNNING node no runner had claimed did take one, and the stop now returns it through TaskGraph.handBackSlotLocked — the slot arithmetic TaskGraph.complete already had, now shared by both."
---

Ask for a harness and quit, and the design used to run on as an orphan: two model
calls of real spend after the quit, a job log landing under a session that had
left, and no kill that could find it. The node became reachable only from inside
its own goroutine, and Close walked a registry the node had not joined yet.

This is the graph's stop at quit, and it is deliberately not Agent.Abandon
(#358), which is one turn's bounded stop at a person's escape and never touches a
node. The two are named beside each other so nobody later unifies them.
