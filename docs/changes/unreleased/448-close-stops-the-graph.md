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
---

Ask for a harness and quit, and the design used to run on as an orphan: two model
calls of real spend after the quit, a job log landing under a session that had
left, and no kill that could find it. The node became reachable only from inside
its own goroutine, and Close walked a registry the node had not joined yet.

This is the graph's stop at quit, and it is deliberately not Agent.Abandon
(#358), which is one turn's bounded stop at a person's escape and never touches a
node. The two are named beside each other so nobody later unifies them.
