// Package sample generates preview records from app.json without accessing
// persisted content. Each record type is named by its nature: people get full
// names, companies get company names, and other records get short noun
// phrases (a descriptive value from their own schema, a neutral qualifier and
// their label), followed by the person or company they refer to. Titles never
// lead with a verb or repeat the type as a prefix. Emails derive from the
// person's name on reserved .example domains; money and numbers take their
// magnitude from field-name hints; longtext names the record. Pipeline enums
// (status, stage, phase) show every state, then favour the middle ones.
// Defaults are respected except for relations, dates, unique fields and enum
// states. The constructor generates twelve records per type. Dates stay near
// the current clock: due, publish, start and renewal fields lie ahead, created,
// started and last-touch fields lie behind; date values use YYYY-MM-DD and
// datetime values use RFC 3339. GenerateAt fixes the clock and seed for
// deterministic fixtures, and Service.Now can inject a clock.
//
// When the services hold a model, Service.Generate first asks it once per
// definition (app, description, the owner's ask) for example titles and text
// values per type; replies are validated against the schema and only then used
// as vocabulary. Any error, or values that break a uniqueness rule, falls
// back to the deterministic generator.
//
// References can point forward or form cycles and remain ids on the wire;
// consumers can read their titles through records.get and the target schema.
// Computed fields use record rules and AI values retain their wire labels.
package sample
