package appwire

// ── the facts a job's page reads, each computed once in the engine ──────────
//
// No methods: factory.queue's jobs[] and factory.job carry these fields, and
// each word says a client may read its fields from this engine. A factory
// older than the word sends none of them, which reads as unknown, never as
// zero (docs/design/app-v3/PROTOCOL.md, "Factory job facts").

// FeatureFactoryJobSource is announced when jobs carry source {name, ref,
// url} (every job: github with `owner/repo#N` and the issue's address, a
// connector's or a trigger's name with the job's key there), repo_url (the
// repository's address read off the forge's own address) and route {rule,
// answer?: {yes, why, model}} (the routing rule that decided the job).
const FeatureFactoryJobSource = "factory.job_source"

// FeatureFactoryJobSteps is announced when jobs carry steps_done and
// steps_total, THE ONE STEP COUNT: the Brief's own counts read the same
// function, and a client says "N of M" from these and from nothing else.
const FeatureFactoryJobSteps = "factory.job_steps"

// FeatureFactoryJobRunsOn is announced when a read job carries runs_on
// {model, from} (its current or next conversation step's model) and each
// unfinished conversation step carries its own: the resolution the runner
// opens the step on, and the rung that named it (step, job, one_model,
// effort, factory, worker, runner).
const FeatureFactoryJobRunsOn = "factory.job_runs_on"

func withFactoryJobFactsFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		hello = withFeature(hello, FeatureFactoryJobSource)
		hello = withFeature(hello, FeatureFactoryJobSteps)
		hello = withFeature(hello, FeatureFactoryJobRunsOn)
	}
	return hello
}
