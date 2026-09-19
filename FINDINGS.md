# Findings

## Item 1: hosted run task reads

This step adds the remote protocol declarations and client/server methods for `PlanTasks` and `PlanTaskPage`, following the existing `PlanSpend` request path. Scripted-client tests will be written failing first and will compare complete `session.PlanTaskRow` and `session.PlanTaskPage` values so every field survives the wire boundary.

The hosted surface receives a wrapped `*remote.Agent`; therefore these read methods must exist on `remote.Agent` and delegate on the server to the engine's session agent. This step does not change the forbidden task-store or TUI implementation paths.
