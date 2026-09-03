// The two streams every door writes to, named once so that a door cannot
// choose the wrong one by accident.
//
// THE RULE, AND IT IS THE WHOLE OF THE RULE: STDOUT IS THE ANSWER — the
// deliverable, the JSON, the rows, the table, the thing a script captures — and
// everything a person reads ABOUT the run goes to stderr: the preamble, the
// progress, the warning, the question, the path a record was kept at.
//
// It matters because the most common thing anybody does with a headless verb is
// put a pipe after it. `aforge plan new "x" --json | jq` breaks the moment a
// `goal:` line is in the stream, `aforge logs | grep -c .` is off by one while a
// path header is the first line, and `aforge cache clean | tee log` used to hand
// the person a blank terminal waiting for a word they could not see — because
// the question had gone into the file with the data.
//
// `aforge do` already kept this exactly (do.go) and it is the standard the rest
// of the binary is held to. [TestNoDoorPrintsItsCommentaryToStdout] reads the
// package with go/ast and names any door that stops keeping it.
package main

import (
	"io"
	"os"
)

// aside is where commentary goes. Read it as the aside in a play: it is said to
// the audience, it is not the scene, and a script that captured the scene never
// hears it.
//
// It is a var and not a constant expression so a test can point it at a buffer.
var aside io.Writer = os.Stderr
