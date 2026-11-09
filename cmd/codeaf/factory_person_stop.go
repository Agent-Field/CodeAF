package main

// AutomaticRefusal answers from the current durable job for every automatic
// wake: the job's document is read again whenever it has changed since the last
// look, and only then, so the ten-a-second watch costs nothing while the floor
// is still (factory_manager_index.go). A person opening or reading the
// conversation does not change that job.
func (d seamRunDoor) AutomaticRefusal(conversation string) string {
	want := sameFileKey(conversation)
	if want == "" || d.st == nil {
		return ""
	}
	return managerIndexOf(d.st).automaticRefusal(d.st, want)
}
