# Common types for apps

## Share contacts between apps or use the same customers in two apps

Common types give your apps a shared vocabulary. A CRM's Contacts can use
Person and its Companies can use Organization. When Support uses those same
types, it refers to the same people and companies by ID. Edits you make with
write access are visible in both apps. Each app still needs its own read or
write access to the records.

During planning, choose the offer to use your existing records. Check uses
example rows in a separate draft. Applying the app connects it to the shared
types and records. Records with similar names are never matched automatically.

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
