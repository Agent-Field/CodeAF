# App connections

## Connect my database or use data from Postgres or MySQL

A personal app can read declared fields from Postgres or MySQL while the rows stay in the database. First keep the
app and attach its type to a common type, for example
`codeaf app connections bind APP contact person`. Use the kept app's ID for `APP`.
The profile operator registers each connection for one app. Put its declaration in a local JSON file and pass the
database connection string on standard input to `codeaf app connections register connection.json`. The profile's
credential store keeps the connection string. Keep it out of the declaration, app files, command arguments and chat.
Registration fixes the connection name and endpoint. Replacing a shared credential requires a new registration and a new source identity namespace. A personal credential can be replaced for the same connection.

A Postgres declaration for CRM contacts can look like this:

```json
{
  "id": "directory",
  "app": "APP",
  "kind": "postgres",
  "reads": {
    "contacts": {
      "type": "contact",
      "table": "contacts",
      "key": "id",
      "version": "version",
      "fields": {"name": "full_name"},
      "match": {"name": "full_name"},
      "order_by": "full_name"
    }
  },
  "writes": {"edit": {"read": "contacts", "fields": ["name"]}}
}
```

For MySQL use `"kind": "mysql"` and a MySQL DSN. Use a database role limited to the declared operations. A writable
table needs a unique stable key and an integer version that every writer advances. MySQL write-back requires InnoDB.

## List connections, test one, or run a declared live read

- `codeaf app connections list APP` gives the connection names, kinds and declared operations for the app.
- `codeaf app connections test APP directory` checks that the source answers.
- `codeaf app connections read APP directory contacts '{"name":"Ada"}'` runs the declared read. Drop the JSON
  argument to read without the optional match.

The engine offers the same operations as `connections.list`, `connections.test` and `connections.read`.
Results carry the connection name, read name and the time of the live observation. Records use canonical IDs.
Ordinary database and HTTP reads retain the identity crosswalk in the local data store.
Known source outages return a refusal. These reads have a five-second deadline, a one MiB payload
budget and at most 500 rows. The CLI returns up to 100 rows and sets
`has_more` if more matched. Narrow a larger result with a declared argument.

For Gmail, Calendar, or Drive sources and their separate sign-ins, see
[Google Workspace in Personal apps](google-workspace-apps.md). A Google read
can declare a bounded window, and a Gmail binding can keep an expiring
metadata index.

## Call an API from my app

Register a connection with `"kind": "http"`, a fixed `origin` such as `https://api.example.com`, and a
`test_path` for the test GET. Pass its bearer token on standard input during registration. A declared read uses
a fixed `path`, and `fields` maps app fields to JSON properties. The response can be an array or an object
containing the array in a named `rows` property. Optional `match` maps read arguments to query parameter names.
Specify the stable source `key` and optionally a source `version` property. Redirects are refused.
Use HTTPS for remote sources. Plain HTTP is available only on loopback.

A declared write can POST to a fixed `path`, sending the key, version when declared, and allowed mapped values
as a JSON object. Its optional `response` mapping selects scalar properties for later run steps. The endpoint
receives the run step's `Idempotency-Key`. When a version is declared, the endpoint must check it before applying
the edit.

## Read public JSON, SEC EDGAR filings or columnar results

A public HTTP connection is registered with `"credential_mode": "public"`
and a generic `"user_agent"` such as `"codeaf public data/1.0"`. Register
from the declaration with no standard input. Public connections use GET
only. Keep the origin and test path fixed in the declaration.

Use a dotted `rows` path to select a nested array, or set `"columnar": true`
when it selects parallel arrays. Include equally long arrays for the key,
mapped fields and any version. For SEC EDGAR submissions, an example is
`"rows": "filings.recent", "columnar": true, "key": "accessionNumber"`.

A public read path can have a placeholder declared in `match`. For example,
`"path": "/submissions/CIK{cik}.json", "match": {"company": "cik"}` uses
the supplied `company` argument in the path. Supply a string to keep leading
zeroes. Path arguments accept 1–64 letters, digits, hyphens or underscores.
Use source keys that are unique across every response to the read.

## Write back to a database or API through an app Action

Declare the exact tool name `connections.directory.edit` in the app's `capabilities.tools`. A screen starts
a declared run with `runs.start`. In the run, use the same tool name and `gate: {approval: send}`.
Its arguments are the canonical row `id`, `version` from `_source_version` when configured, and
`set` with the allowed app fields. The broker asks before dispatch and saves an effect receipt in
`runs.get` under `effects`. A stale version reports a conflict. If a write's outcome is unknown,
the broker leaves the run incomplete so it can be checked at the source before anyone sends it again.
Check uses a sample reply for the exact source tool declared by the app. Keep and register the connection before running an actual write.

## Copy my data in from Postgres

Use a Personal PostgreSQL connection whose declared read includes
`"residency": {"updated_at": "updated_at", "tombstone": "deleted", "fresh_for_seconds": 30}`.
By registering this option, the profile operator approves the declared rows and fields as a fixed export to
the type's existing audience. Use a shared credential for a role and table that export only that
data. Types with row policies and connections using per-person credentials stay on declared live
reads. Review masked fields before copying their values.
The source needs a unique stable key, a managed integer version, a non-null timestamp and a Boolean
tombstone. Every source writer advances the version and timestamp, including on deletion. Keep tombstones
for the mirror to observe. A declared reference may use `residency.references` to map source keys to
existing canonical record IDs, keyed by app field name.

1. Run `codeaf app residency show APP TYPE` to see the active mode.
2. Run `codeaf app residency preview APP TYPE mirror` to see row counts, conflicts and a confirmation token.
3. Apply that plan with `codeaf app residency apply APP TYPE mirror --confirm TOKEN`.

The live read uses a five-second deadline and caches no rows. Live contacts have an empty local history. Live fields stay masked when reveal is requested. Mirror copies a bounded snapshot first and
then polls for edits with a five-minute overlap. Each snapshot or poll is limited to 10,000 rows and one
MiB. Split a larger export into smaller bound types to bring it in. Narrow a live query if its eligible scan exceeds 500 rows. The app version, canonical record IDs and company links stay the same. Set up a live binding on an empty type. The Personal engine catches
up when it starts. The owner wire uses `residency.show`, `residency.preview` and `residency.cutover`.

## Keep it in sync with the old system

A mirror result includes its source, observation time and freshness. A mirror beyond its configured
freshness bound is labeled stale. Contacts lists whose source can change receive a payload-free
`data.reset` and refetch under the current grant. A source tombstone removes a row from the mirror.
Use the brokered app Action to edit the source in mirror mode. Give it the row's `_source_version`.
The broker saves its result and reports a conflict if that source revision changed. Synchronization
records the observed source revision and preserves values and clears a person already pinned.

The poll expects committed updates within the five-minute overlap and treats hard deletes or a
missing key during cut-over as conflicts. Logical replication is optional and this connection uses polling.

## Stop depending on the old system

Pause upstream writers before moving a mirror to native. Preview with
`codeaf app residency preview APP TYPE native --source-quiesced`. Apply with
`codeaf app residency apply APP TYPE native --source-quiesced --confirm TOKEN`.
A changed plan or a conflict requires a fresh preview. Once native, ordinary edits to fields with
a declared reverse mapping go to codeaf's writer. During the reversible window, creates, deletes
and fields without a reverse mapping are fenced. Native reads continue while the source is offline.

To return from native, pause upstream writers again and preview `live` or `mirror`.
The preview includes the number of rows whose native edits need replay. Apply conditionally writes
those edits back before switching the read route. Upstream conflicts keep the native route and its
edits in place. A mirror can return directly to live with a fresh preview and confirmation.
Returning to live retires the copied values and keeps the identity crosswalk and audit receipts.

## Use my own source credential or run the connection as me

Put `"credential_mode":"person"` in the connection declaration and pass
your credential on standard input when registering it. To connect or replace
your credential later, pass it on standard input to
`codeaf app connections credential APP CONNECTION`. A personal connection
uses your own source account for reads, tests and approved sends. Source
permissions determine which records can reach the app, and the app's record
policy can narrow those results further.

If the source rejects the request, the app says “the source refused access
with your credential”. Attach or renew the source credential for the local
profile if its access has expired. A live row-policy type can be read through
a mapped owner field. Write-back for that type requires source support for
checking the same row policy during the update.

## Connect Microsoft 365, Outlook, OneDrive or Teams to an app

Use a Microsoft public client registration and your profile's browser connection. See the Microsoft 365
apps page for scopes, delta cursors, content review, triggers and approval-gated Outlook or Calendar Actions.
