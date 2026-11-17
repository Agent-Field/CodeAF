package appwire

// FeatureFactoryJobStepEditWhen is announced beside FeatureFactoryJobStepEdit
// when added conversation, check and approval steps may carry a condition.
// Apps send an added step's when only to engines announcing this feature.
const FeatureFactoryJobStepEditWhen = "factory.job_step_edit_when"

func withFactoryJobStepEditWhenFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobStepEditor); ok {
		hello = withFeature(hello, FeatureFactoryJobStepEditWhen)
	}
	return hello
}
