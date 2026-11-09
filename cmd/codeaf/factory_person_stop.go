package main

// AutomaticRefusal reads the current durable job for every automatic wake.
// A person opening or reading the conversation does not change that job.
//
// Every open job conversation asks this ten times a second, so it reads only
// what changed: the manager index decodes a job document again only when its
// file changed, and the job this conversation manages is checked against its
// file on every ask.
func (d seamRunDoor) AutomaticRefusal(conversation string) string {
	want := sameFileKey(conversation)
	if want == "" || d.st == nil {
		return ""
	}
	stop, ok, err := managerIndexOf(d.st).personStop(d.st, want)
	if err != nil {
		return "job could not be read; automatic work stopped"
	}
	if !ok {
		return ""
	}
	return stop
}
