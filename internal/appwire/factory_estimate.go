package appwire

// FeatureFactoryEstimate is announced in hello by an engine whose factory
// reads issues with the complexity rubric and measures cost from its own
// finished jobs: a job's triage carries `rubric` and never `est`, an open job
// carries `estimate` once five similar jobs finished, and the queue's
// `desk.estimates` says how many jobs the figures see and their hit rate. A
// desk that does not see it treats a missing figure as an older factory, not
// as "no cost history yet".
const FeatureFactoryEstimate = "factory.estimate"

func withFactoryEstimateFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactoryEstimate)
	}
	return hello
}
