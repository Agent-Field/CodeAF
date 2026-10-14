// Package sample generates preview records from app.json without accessing
// persisted content. Titles combine type labels, descriptive schema vocabulary
// and short neutral phrases without classification codes or counters; human names
// fill person-like text, while person/people fields keep member ids. Defaults
// are respected except for relations, dates, unique fields and enum states.
// The constructor generates twelve records per type. Dates stay near the
// current clock, with start and upcoming fields in the future; date values
// use YYYY-MM-DD and datetime values use RFC 3339. GenerateAt fixes the clock
// and seed for deterministic fixtures, and Service.Now can inject a clock.
// References can point forward or form cycles and remain ids on the wire;
// consumers can read their titles through records.get and the target schema.
// Computed fields use record rules and AI values retain their wire labels.
package sample
