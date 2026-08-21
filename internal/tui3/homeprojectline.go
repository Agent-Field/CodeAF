package tui3

// STUB FOR THE MERGE: the layout lane is making a project a cursor stop, and
// this is the name its row kind will have — repoint this constant at that kind
// and delete this file.
//
// It is deliberately a value NO REAL KIND HAS, so the case
// [app.homeSubject] carries for it is inert until the merge rather than
// stealing a row from a kind that does exist. Nothing draws it and nothing
// builds it; it exists so the project card's bands (homeband_project*.go) have
// a subject to be about the moment the left column can produce one.
const homeProjectLineKind homeRowKind = 200
