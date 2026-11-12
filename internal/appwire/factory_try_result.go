package appwire

// FeatureFactoryTryResult is announced in hello when this engine's job reads
// (factory.job, and the steps in factory.job_step_edits_read) carry how each
// finished try ended: steps[].rounds[].result, the step's rule word (clean,
// not clean, green, not green, proven, not proven, done, not done, blocked),
// and steps[].rounds[].reason, a counted reason ("2 findings"). Both come from
// the try's structured report, never its prose (factory.TryResult). A try
// recorded before them, one that ended without a report, and every try read
// from an attached factory older than this engine carry neither, and a surface
// then shows the try's number alone.
const FeatureFactoryTryResult = "factory.job_try_result"

func withFactoryTryResultFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactoryTryResult)
	}
	return hello
}
