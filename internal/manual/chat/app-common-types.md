# Common types for apps

## Share contacts between apps or use the same customers in two apps

Common types give your apps a shared vocabulary. A CRM's Contacts can use
Person and its Companies can use Organization. When Support uses those same
types, it refers to the same people and companies by ID. Edits you make with
write access are visible in both apps. Each app still needs its own read or
write access to the records. In Desk, a shared record list shows the fields
from the shared type. Select a record name to open it. With write access,
select a field such as Email, change it and press Enter. The other open apps
show the change as it is saved.

With Desk connected, choose the offer to use your existing records during
planning. When the revised outline appears, select **Write it** to build the
screens. If Check discovers another shared type, the build pauses and asks
whether to use those records. Choose in the outline to continue. **Keep**
appears after the engine checks the written screens. A source-less starter such
as “Start a People list” makes a list for this app.

For a headless build, the plan lists matching reuse as a proposal,
such as “Uses your existing Contacts from CRM,” and Build continues to Check.
Check uses example rows in a separate draft. Apply confirms reuse and connects
the app to the shared types and records. Applying a build leaves those records
as they are.

## List the common types or bind an existing type

Use `codeaf app parts` for the kit catalog, or `codeaf app common-types` for
the versioned common types library. It lists each type's key, field kinds,
roles and links to other common types. The library includes Person,
Organization, Address, Money, Document, Product, Location, Team, Event
(including meetings) and Task.

To adopt an existing Contacts type as Person, use
`codeaf app bind-common-type '{"common":"person","type":"contact"}'`
for the same profile as the desktop engine. The receipt includes the
unchanged type ID. A common key can point to one type in your space. If a
different type already uses the key, the command refuses the change.

An app definition in kit 2 can declare a common key on a record type, for
example `"common":"person"`. Use `"uses":true` when adopting an already bound
type into a new draft. The draft can keep its own type alias. For declared Actions that create or
edit the shared records, give that alias `"rw"` in `capabilities.records`.
The builder can set this with `"access":"rw"` on `add-type`.

## Duplicate contacts, same customer in two systems, or merge records

Open the desktop engine for your profile. To look for duplicates of one
published Person or Organization record, run
`codeaf app match-scan '{"type":"contact","id":"<record ID>"}'`.
Then run `codeaf app match-candidates`. Each result gives the two record IDs,
their revisions and the signals that prompted review. The same review also
appears in My Work. Compare the records before choosing a result.

- Use `codeaf app match-accept '{"id":"<review ID>","survivor":"<record ID>"}'`
  to join the two records and keep the selected ID as the survivor.
- Use `codeaf app match-reject '{"id":"<review ID>"}'` to keep records
  separate. The decision is remembered on later scans.
- If you already know two records should join, use
  `codeaf app match-merge` with `canonical`, `alias`, `canonical_revision` and
  `alias_revision`.

An old ID still opens the survivor and existing links keep working. A value
or clear entered by a person takes precedence. If both people-entered
values differ, the survivor's value wins, and the other value remains in
the other record's history. To restore the two records, use
`codeaf app match-unmerge '{"id":"<merge ID>","revision":<current survivor revision>}'`.
If the source joined the records automatically, use the merge batch ID from
record history. Split newer merges on the same survivor first.

When a source is configured to copy values into this space, a strong,
unambiguous match may join automatically. Shared email addresses,
ambiguous matches and person-entered conflicts go to review. Organization
domains match organizations, and a similar name alone does not join
records. Live connections keep their existing source visibility.
