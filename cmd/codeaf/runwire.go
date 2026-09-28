package main

// The v3 chat's task door reaches the run engine through a seam the session
// package owns, and the engine installs itself at load. Linking the engine is
// what puts the door's second road — a `/task` under the bash belt starting a
// RUN rather than a node of the session's own tree — into this binary, so the
// import is a blank one: nothing here calls the package, and the registration is
// its one effect. With CODEAF_TASK_BELT naming the older belt the door answers
// the road it always had, and the import still changes nothing a person sees.
import _ "github.com/Agent-Field/codeaf/internal/run"
