package appwire

// FeatureFactoryJobStepEditMax is announced beside [FeatureFactoryJobStepEdit]
// when this engine carries `max` in a step edit: how many tries one
// conversation step of one job gets, 1 to 3. The factory takes it only from
// the job's person and only for a conversation step that has not run, is not
// fixed and does not come after proof; a check's rounds are said when it is
// added, and an approval does not loop. Every job read says it as
// steps[].max. An app sends `max` to no engine without the feature, and shows
// Max tries read-only there (from steps[].max or steps[].round_limit).
const FeatureFactoryJobStepEditMax = "factory.job_step_edit_max"

func withFactoryJobStepEditMaxFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobStepEditor); ok {
		hello = withFeature(hello, FeatureFactoryJobStepEditMax)
	}
	return hello
}
