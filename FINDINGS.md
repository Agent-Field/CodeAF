# Findings

## Item 1: hosted run task reads

This step adds the remote protocol declarations and client/server methods for `PlanTasks` and `PlanTaskPage`, following the existing `PlanSpend` request path. Scripted-client tests will be written failing first and will compare complete `session.PlanTaskRow` and `session.PlanTaskPage` values so every field survives the wire boundary.

The hosted surface receives a wrapped `*remote.Agent`; therefore these read methods must exist on `remote.Agent` and delegate on the server to the engine's session agent. This step does not change the forbidden task-store or TUI implementation paths.

### Scripted-client contract

The client harness can return typed scripted answers by method name. The regression test uses `reflect.DeepEqual` on complete row and page values, including nested rows, notes, steps, live-step data, timestamps, slices, and the page's presence boolean; adding a field later without carrying it will make equality fail.

### Implementation and focused verification

The protocol now has separate getter-class methods for the row list and task page. The client returns the honest empty values on an unavailable or malformed answer; the server delegates only when its wrapped agent exposes the corresponding session methods, otherwise preserving the same empty reading.
