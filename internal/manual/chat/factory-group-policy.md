# Factory group policy and allowances

## group policy, allowances and automatic starts after waits

A job can carry an opaque `group`, set by `factory.job_create`. It is trimmed,
at most 200 characters, with no control characters. `create_group` says the
engine carries it. `factory.policy_get` reads a group's policy;
`factory.policy_set` replaces its stopped flag and allowances. Both take
`factory` and a non-empty `group`. HTTP uses GET and PUT on
`/v1/groups/{group}/policy`, with the group escaped as one path segment.

A released waiting job in Backlog starts automatically only when its policy
allows it. An allowance must list the job, end in the future and have room for
the job's whole budget after recorded spending. The automatic start names the
allowance in activity. The factory's intake cost limit does not apply. Without
an allowance it stays in Backlog with `blocked.by: policy`, for example
`no allowance covers it`. A later grant is checked on the next waits scan.
Your Start overrides an allowance's scope, cap and end, and the waits. The job's
own lifetime budget still applies.

The engine advertises `factory.group_policy` and capability `group_policy`.
Use it when the connected factory's `factory.features` also lists
`group_policy`. An older factory returns a conflict with reason
`factory_too_old`. Group policies survive a factory restart.

## policy limits, allowance spending, end dates and versions

A policy has at most 50 allowances. Each allowance has a distinct `id` of
1–64 characters from letters, digits, dot, underscore, colon and hyphen;
1–200 distinct positive job ids; `cap_usd` greater than zero and at most
10,000; and an RFC 3339 `ends_at`. Past end dates are accepted. An empty
allowance list revokes the grants. The job ids belong to this factory.

Reads include group `spent_usd` across every job in the group. Each allowance's
`spent_usd` counts every run of its listed jobs that exist here, including
earlier runs and runs a person started. Its `state` is `ended` at the end date,
else `spent` at the cap, else `active`. An unset group returns an empty policy
and its group spending. Reads include `version`; send that optional version
with a replacement to reject stale changes with a conflict.

On a team factory the first setter owns the policy. Only that owner or the
operator may replace it, and it automatically starts only jobs the owner may
start. A solo factory skips ownership checks. Every replacement is recorded
in factory activity.

## policy hold, stopped groups and Continue or Start

A policy's run is checked again at lease admission, step begin and worker
reports. When its allowance ends, reaches its cap or stops covering the job,
the job becomes needs-you with `blocked.by: policy`. Its cap question states
the policy reason. Its work and spending stay saved, and execution is
withdrawn. Continue or Start resumes the work as your run; allowance caps,
ends and scope then stop checking that run.

Setting `stopped: true` refuses every run in the group, including one you
started, with `its group is stopped`. Running jobs get Stop after this step;
queued jobs meet the lease gate. Clearing stopped allows new automatic starts
of covered releases. Jobs already held wait for a person to Continue or Start.
An unreadable policy file holds with `its group's policy cannot be read` and
prevents automatic starts.
