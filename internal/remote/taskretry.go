package remote

import "errors"

// The capability keeps the retry key absent on engines without this door.
type taskRetryDoor interface{ RetryTask(uint64) error }

func taskRetryKnown(agent any) bool       { _, ok := agent.(taskRetryDoor); return ok }
func (a *Agent) TaskRetrySupported() bool { return a.c.Welcome().TaskRetry }

// RetryTask binds the task number to the conversation shown by this connection.
func (a *Agent) RetryTask(id uint64) error {
	return a.RetryTaskIn(id, a.c.Welcome().SessionFile)
}

// RetryTaskIn retains the page's owner even if the connection changes before the command runs.
func (a *Agent) RetryTaskIn(id uint64, conversation string) error {
	welcome := a.c.Welcome()
	if !welcome.TaskRetry {
		return errors.New("retrying tasks needs a newer engine; update the engine and reconnect")
	}
	_, err := a.c.call(nil, MethodTaskRetry, TaskSetupArgs{ID: id, Session: conversation})
	return err
}
