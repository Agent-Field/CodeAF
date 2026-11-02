package appwire

// ── factory.job_protected: which protected paths a Backlog job names ───────
//
// No method: factory.queue's jobs carry `protected {paths}` on each Backlog
// job (internal/factory/api's JobProtected), the protected paths of the
// rules it would start under that its issue, comments, plan or files name.
// Absent on a job is not judged (past the Backlog, or rules nobody could
// read), never "touches none"; an empty list is a job that names none. A
// factory older than this word sends nothing, which reads the same.

// FeatureFactoryJobProtected is announced in hello by an engine whose
// factory's queue carries [api.JobProtected] on Backlog jobs.
const FeatureFactoryJobProtected = "factory.job_protected"

func withFactoryJobProtectedFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactoryJobProtected)
	}
	return hello
}
