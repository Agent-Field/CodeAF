package appwire

import "reflect"

// EventFactoryJobsChanged says a job arrived or changed state on a factory's queue, so a page showing that queue reads it again now instead
// of on its next slow refresh. Its data is [FactoryJobsChanged].
//
// IT CARRIES WHERE, NOT WHAT. The queue is still read with factory.queue; the
// event only says which factory and which job, so a page that is not looking
// at that factory ignores it and nothing here can drift from the read.
//
// It is emitted when an AgentField trigger files a job on this machine's
// factory (internal/factory/agentfield), once on filing and once after the
// intake pass that may start it. Committed state, question and completion
// changes on the local factory also emit it; progress-only writes do not.
const EventFactoryJobsChanged = "factory.jobs_changed"

// FactoryJobsChanged is the job that changed and the factory it is on.
// Factory is the id factory.queue takes.
type FactoryJobsChanged struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
}

func init() {
	eventShapes[EventFactoryJobsChanged] = reflect.TypeFor[FactoryJobsChanged]()
}
