# Ask on a part: required contract repair

The part Ask feature is blocked by the current R4 edit boundary. This file
records the required repair; it does not announce an implemented capability.

## The request cannot reach the agent

`bridge.AgentSendParams` is declared in `internal/apps/bridge/proto.go`.
It contains only `Thread`, `Text`, `Record`, and `Screen`. Go cannot add a
field to an existing struct from another file. An extension struct declared
in `proto_surfaces.go` does not change that type or the `svc.Agent.Send`
signature.

The `agent.send` registration in `internal/apps/gateway/methods.go` decodes
directly into `bridge.AgentSendParams`. JSON decoding ignores an incoming
`part` field. An extension alone therefore cannot deliver the document,
part, or base version to the agent.

## Comments lose both the part reference and the anchor

`sendComment` in `internal/apps/gateway/surfaces.go` calls `Agent.Send`
with a prose description and `Record`. It does not pass the comment anchor
or a structured part reference. Inferring the target from these strings
would be a workaround for the contract, not the specified Ask path.

## Minimum repair required from the owning lanes

1. In `proto_surfaces.go`, define the extension fields, including optional
   `part {doc, part, version}` and the comment anchor.
2. In `proto.go`, embed that extension in `AgentSendParams`. This one
   embedding line is explicitly prohibited by the current task and needs
   an amended ruling or a contract-lane change.
3. In gateway `sendComment`, load the current part version and pass the
   structured reference, the original comment text, and its anchor. This
   requires authorization for the gateway lane's handler.

With the embedding repair, the existing `agent.send` decoder and
`svc.Agent.Send` signature can remain unchanged. The agent can then read
the extension using its allowed registration hook.

## Cancellation seam already available

The concrete collaboration service exports
`OnAgentStop(func(app, doc, part string))`; `svc.Collab` does not include
that method. Agent code can use a small local interface assertion without
changing `svc.go`. Registration must happen after collaboration is wired,
because `server.go` constructs the agent before collaboration.

After the contract repair, the original acceptance remains required:
agent-labelled v+1, a lock held through the turn and released before the
terminal event, takeover cancellation without a write, one format retry,
and definition-path rejection. No live model call is useful until the
structured request can reach that implementation.
