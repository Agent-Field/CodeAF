// Package data is the person's own data store for fluid apps: one SQLite file
// per person at <codeaf home>/data/data.db, holding every record of every
// type, whichever app made it.
//
// DATA BELONGS TO THE PERSON, NOT TO THE APP. A record type has an owner app
// (the one that declared it) and may be used by others, which add their own
// fields; deleting or archiving an app never deletes a record, and removing a
// field from an app hides it and never deletes a value. Every change writes a
// history row under a batch id, and a batch can be undone as long as nothing
// has changed the same records since.
//
// The store has ONE WRITER GOROUTINE and a pool of readers. Every write, from
// a screen, the app's agent, a run or a CSV import, is a closure handed to the
// writer, so validation, uniqueness, freeze rules and the history row are
// decided against exactly the state the write commits on. Readers never wait
// on the writer: the file is in WAL mode.
//
// Queries are the QueryField tokens a person types ("stage:Proposal,Won
// owner:me amount>10000 sort:-amount"). They are parsed into terms over the
// type's declared fields and compiled into parameterised SQL. EVERY VALUE IS
// A BOUND PARAMETER; the only words spliced into SQL are field and type names,
// and those are identifiers the registration checked against a strict pattern
// and the query compiler checks again before it uses them.
//
// Every call made on behalf of an app is checked against that app's declared
// capabilities first ("records": {"deal": "rw"}); a call with no app is the
// person in the Data place and may do everything. A draft (a Builder working
// folder) has its own sample database with the same API.
package data
