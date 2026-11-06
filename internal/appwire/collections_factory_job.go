package appwire

// FeatureCollectionsFactoryJob names an engine whose collections hold
// factory_job references: a Factory job by its factory and job number. A
// client requires it before it relies on adding a job to a collection: an
// engine that announces only "collections" may refuse the kind with bad_params.
const FeatureCollectionsFactoryJob = "collections.factory_job"

// withCollectionsFactoryJobFeature is hello carrying collections.factory_job
// when the engine serves collections: this wire checks the factory_job kind
// and passes it to every collector.
func withCollectionsFactoryJobFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Collector); !ok {
		return hello
	}
	return withFeature(hello, FeatureCollectionsFactoryJob)
}
