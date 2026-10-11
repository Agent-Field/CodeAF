# Desktop chat drift

## Can a chat that changes subject earn more homes?

The Places organiser can weigh where a chat began and where it ended.
Its stream drift stage keeps the chat's recorded primary home and returns
extra homes wherever either anchor clears the similarity floor and stays
within the earned-home margin of that anchor's best match. There is no
numeric cap. Existing homes are kept; places in the same branch count once.

An absent ending is no evidence: a single-message chat does not get an END
anchor. A chat in Now still needs its first filing by its start anchor.

## Does a chat changing subject move its primary home automatically?

No. Drift returns a proposal for a different primary home. The person must
accept that proposal before the primary moves. Extra homes and a proposed
primary move are separate operations.

## When does desktop chat drift run after a settled reply?

The bridge has an organiser hook after its existing settled-turn wait.
Interrupted streams do not reach it. Later settled replies reach it even
when the initial filing ledger already considered the chat. The ending is
built from real later user messages and the last uninterrupted assistant
reply, excluding tool output.

The hook requires an organiser caller that supplies its tree, cached anchors
in one embedding space, and the policy and proposal application. This
checkout's desktop startup does not yet configure that caller: automatic
live drift is absent until the organiser integration connects it. The stream
stage itself owns no timer and opens no model or embedding connection.
