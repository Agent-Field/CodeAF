# App assistant

## Ask about app records, start an automation, or draft a message

Open the assistant from an active app.

The assistant's `app` tool uses the same app command as `codeaf app`. It can
list the app's declared views and actions, query a view with its named inputs,
and invoke a granted action. The agent starts with no read or action grants.
Grant each named view or action you want it to use. It reads declared views
with scalar inputs and at most one sort field. Masked fields stay hidden.
The assistant's writes carry AI provenance, and a value you set or clear
takes precedence.
An outgoing action waits for you to review and approve its exact payload.

## Find and grant a write action such as Mark Won

Record-changing runs appear in the action list by run ID, or by command ID
when a command launches the run. App agent settings lists the same grant
choices. In the CLI, use `codeaf app show '{"app":"APP_ID"}'` to find each
Action ID and its typed arguments. List grants with `codeaf app agent_query
'{"app":"APP_ID","kind":"grants"}'`. To grant Mark Won, use `codeaf app
agent_grant '{"app":"APP_ID","agent":"APP_ID","group":"actions","id":"mark-won","allowed":true}'`.
When using another agent, put its principal ID in `agent`. Its grants are
separate. Grant the assistant a Deals view as well so it can find a deal by
name. Check reports any Action ID promised in `plan.agent.actions` that
the app does not declare. A run that uses an unavailable model or harness
is omitted from the live Action and grant lists until it can run.

## Add a field, column, screen or automation from the app assistant

Ask for the change in your own words. The assistant uses the `propose_change`
app command to hand just those words to a fresh editing session when it has
the Propose changes grant. A checked change appears on a **Proposed change**
card that stays available after restarting. The card previews a change to
one screen in place. For a larger change, choose **Open in builder** to see
all affected screens. Choose
**Apply** to publish a new version or **Discard** to keep the current one.

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
