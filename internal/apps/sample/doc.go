// Package sample generates preview records from app.json without accessing
// persisted content. Each record type is named by its nature: people get full
// names, companies get company names, and other records get short noun
// phrases (a descriptive value from their own schema, a neutral qualifier and
// their label), followed by the person or company they refer to. Titles never
// lead with a verb or repeat the type as a prefix. Emails derive from the
// person's name on reserved .example domains; money and numbers take their
// magnitude from field-name hints; longtext names the record. Pipeline enums
// (status, stage, phase) show every state, then favour the middle ones.
// Text defaults are respected; relations, dates, enums, numbers, money,
// checkboxes and unique fields vary. A client or customer with a contact
// person is a company; number-titled records read "Invoice #1001". Rows then
// cohere: due dates follow issue dates, outcome dates (paid on) appear only
// once the status reached them, late rows are past due and unpaid, and
// computed fields are evaluated from the generated inputs. Row counts cap at
// a type's unique value space, and only generated text titles must be
// distinct. The constructor generates twelve records per type. Dates stay near
// the current clock: due, publish, start and renewal fields lie ahead, created,
// started and last-touch fields lie behind; date values use YYYY-MM-DD and
// datetime values use RFC 3339. GenerateAt fixes the clock and seed for
// deterministic fixtures, and Service.Now can inject a clock.
//
// When the services hold a model, Service.Generate first asks it once per
// definition (app, description, the owner's ask, the plan's data lines, every
// type with its fields, enum values and refs, and today's date) to write whole
// rows per record type. The reply is cached per definition and coerced field
// by field: enum membership, ref titles resolved to ids, day offsets ("+3d
// 14:30") turned into dates around the injected clock, .example addresses,
// unique rules and row caps. A value that does not fit is dropped and only
// that field comes from the generator; computed fields are always evaluated.
// A timeout, an error or an unusable reply gives the deterministic sample.
//
// References can point forward or form cycles and remain ids on the wire;
// consumers can read their titles through records.get and the target schema.
// Computed fields use record rules and AI values retain their wire labels.
package sample
