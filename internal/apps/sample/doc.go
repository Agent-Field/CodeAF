// Package sample generates deterministic preview records from app.json without
// accessing persisted content. The constructor generates twelve records per
// type; Generate accepts a count and seed for Check fixtures. References can
// point forward or form cycles, computed fields use the record rules, and
// generated AI values retain their wire labels.
package sample
