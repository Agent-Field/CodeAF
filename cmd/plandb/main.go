// Command plandb is the plan store's command line (docs/design/plandb-cli
// /DESIGN.md): the coordination verbs a bash-belt worker runs through its one
// bash tool, over the same store the runtime dispatches from. The whole
// runner lives in internal/plandb so `codeaf plandb` reaches the identical
// Main as this binary — this file is the sibling spelling of one door.
package main

import (
	"os"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func main() {
	os.Exit(plandb.Main(os.Args[1:]))
}
