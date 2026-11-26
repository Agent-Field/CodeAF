# App assistant

## Ask about app records, start an automation, or draft a message

The assistant's `app` tool uses the same app command as `codeaf app`. It can
list the app's declared views and actions, query a view with its named inputs,
and invoke a granted action. The agent starts with no read or action grants.
Grant each named view or action you want it to use. It reads declared views
with scalar inputs and at most one sort field. Masked fields stay hidden.
The assistant's writes carry AI provenance, and a value you set or clear
takes precedence.
An outgoing action waits for you to review and approve its exact payload.

## Add a field, column, screen or automation from the app assistant

Ask for the change in your own words. The assistant uses the `propose_change`
app command to hand just those words to a fresh editing session. A checked
change to one screen can be previewed and applied from the card. For larger
changes, use Open in builder to see the draft. The published app changes
when you press Apply.

## Ask the agent or schedule an app duty

Ask the agent from an app screen or a specific record. The question creates
an app run, and the answer appears with that run. A standing duty repeats on
a five-field cron schedule and runs with the same agent's grants.

From the local profile, `codeaf app agent_ask`, `agent_query`, `agent_grant`,
`agent_duty_add`, and `agent_duty_remove` take JSON. Use `agent_query` with
`{"app":"APP_ID","kind":"duties"}` to find a standing duty's ID. Use
`agent_duty_remove` with the app and duty IDs to stop future occurrences.
For a data grant, pass `app` and `agent` as the app ID, `group` as `data`,
`id` as the view's `screen/view` ID, and `allowed` as true.

The `show`, `query`, and `invoke` commands
take an app ID and optional `params` containing the content command inputs.
For example, `codeaf app query '{"app":"APP_ID","params":{"view":"deals/open"}}'`.
The CLI `propose_change` command takes an app ID and your `request` text.
