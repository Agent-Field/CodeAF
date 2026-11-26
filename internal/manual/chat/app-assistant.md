# App assistant

## Ask about app records, start an automation, or draft a message

The assistant's `app` tool uses the same app command as `codeaf app`. It can
list the app's declared views and actions, query a view with its named inputs,
and invoke an action the app grants. Masked fields stay hidden. The assistant's
writes carry AI provenance, and a value you set or clear takes precedence.
An outgoing action waits for you to review and approve its exact payload.

## Add a field, column, screen or automation from the app assistant

Ask for the change in your own words. The assistant uses the `propose_change`
app command to hand just those words to a fresh editing session. Review the
change in the resulting card. The published app changes when you press Apply.

## Ask the agent or schedule an app duty

Ask the agent from an app screen or a specific record. The question creates
an app run, and the answer appears with that run. A standing duty repeats on
a five-field cron schedule and runs with the same agent's grants.

From the local profile, `codeaf app agent_ask`, `agent_query`, `agent_grant`,
and `agent_duty_add` take JSON. The `show`, `query`, and `invoke` commands
take an app ID and optional `params` containing the content command inputs.
For example, `codeaf app query '{"app":"APP_ID","params":{"view":"deals/open"}}'`.
The CLI `propose_change` command takes an app ID and your `request` text.
