package appwire

// FeatureFactoryJobStepEditModel is announced beside [FeatureFactoryJobStepEdit]
// when this engine carries `model` in a step edit: the model one step of one
// job runs on, checked against this engine's models.list before it is sent.
// The factory takes a pick only from the job's person, only for a
// conversation step (a check and an approval run no model) that has not run.
// Every job read then says the pick as steps[].use_model; steps[].model stays
// the model the step ran on. An app sends `model` to no engine without the
// feature, and keeps a step's model read-only there.
const FeatureFactoryJobStepEditModel = "factory.job_step_edit_model"

func withFactoryJobStepEditModelFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobStepEditor); ok {
		hello = withFeature(hello, FeatureFactoryJobStepEditModel)
	}
	return hello
}
