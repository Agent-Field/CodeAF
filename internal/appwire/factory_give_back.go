package appwire

// FeatureFactoryGiveBackChecks is announced beside [FeatureFactoryJobControl]
// when giving back a job the person took over (factory.job_control mode
// resume_pipeline after take_over) checks the person's commits again: before a
// pull request is open, the run goes back to the first check or review that
// had already passed and runs it and every step after it, and every step reads
// that the branch changed by hand. The job page offers Take over only where
// this is announced; elsewhere it offers opening the branch in an editor.
const FeatureFactoryGiveBackChecks = "factory.job_give_back_checks"
