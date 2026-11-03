package main

import "github.com/Agent-Field/codeaf/internal/factory"

// AutomaticRefusal reads the current durable job for every automatic wake.
// A person opening or reading the conversation does not change that job.
func (d seamRunDoor) AutomaticRefusal(conversation string) string {
 it, err := d.managed(conversation)
 if err == factory.ErrNotManager { return "" }
 if err != nil { return "job could not be read; automatic work stopped" }
 if err := factory.CheckPersonStop(it); err != nil { return err.Error() }
 return ""
}
