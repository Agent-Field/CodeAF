package run

import "github.com/Agent-Field/codeaf/internal/session"

// ChatEngine is the engine this package installs into the chat's task door,
// reached by a test the way the door reaches it: through [session.RunEngine].
var ChatEngine session.RunEngine = engine{}
