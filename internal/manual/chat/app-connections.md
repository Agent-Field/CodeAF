# App connections

## Connect my database or use data from Postgres or MySQL

A personal app can read declared fields from Postgres or MySQL while the rows stay in the database. First keep the
app and attach its type to a common type, for example
`codeaf app connections bind APP contact person`. Use the kept app's ID for `APP`.
The profile operator registers each connection for one app. Put its declaration in a local JSON file and pass the
database connection string on standard input to `codeaf app connections register connection.json`. The profile's
credential store keeps the connection string. Keep it out of the declaration, app files, command arguments and chat.
Registration fixes the connection name, endpoint and credential in this version. A replacement needs a new name and
gets a new source identity namespace.

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
Only the identity crosswalk stays in the local data store. Known source outages return a refusal.
Reads have a five-second deadline, a one MiB payload budget and at most 500 rows. The CLI returns up to 100 rows
and sets `has_more` if more matched. Narrow a larger result with a declared argument.

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

## Write back to a database or API through an app Action

Declare the exact tool name `connections.directory.edit` in the app's `capabilities.tools`. A screen starts
a declared run with `runs.start`. In the run, use the same tool name and `gate: {approval: send}`.
Its arguments are the canonical row `id`, `version` from `_source_version` when configured, and
`set` with the allowed app fields. The broker asks before dispatch and saves an effect receipt in
`runs.get` under `effects`. A stale version reports a conflict. If a write's outcome is unknown,
the broker leaves the run incomplete so it can be checked at the source before anyone sends it again.
