package appwire

// FeatureFactoryJobArea is announced in hello when this engine's factory jobs
// (factory.queue's jobs[] and factory.job) carry area: the part of the
// repository the job's change touched most, the repository's declared area
// or else the top-level directory with a trailing slash ("cli/"). A job that
// changed no file, one that changed root files alone, and every job read from
// an attached factory older than this engine carry none.
const FeatureFactoryJobArea = "factory.job_area"

func withFactoryJobAreaFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactoryJobArea)
	}
	return hello
}
